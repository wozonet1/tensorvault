package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/ingester"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/types"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DataService struct {
	tvrpc.UnimplementedDataServiceServer
	app *app.App
}

func NewDataService(application *app.App) *DataService {
	return &DataService{
		app: application,
	}
}

// =============================================================================
// 0. Pre-check (Optimistic Deduplication)
// =============================================================================

// CheckFile 实现了“双阶段上传”的第一阶段
// 客户端提供文件的 LinearHash 和 Size，服务端检查是否已有对应索引
func (s *DataService) CheckFile(ctx context.Context, req *tvrpc.CheckFileRequest) (*tvrpc.CheckFileResponse, error) {
	// 1. 参数转换与校验
	linearHash := types.LinearHash(req.Sha256)
	if !linearHash.IsValid() {
		// 尽管 Proto 有 validate，这里是最后一道防线
		return nil, status.Error(codes.InvalidArgument, "invalid sha256 format")
	}

	// 2. 查询元数据索引
	// s.app.Repository 是我们在 Step 2 中增强过的
	idx, err := s.app.Repository.GetFileIndex(ctx, linearHash)
	if err != nil {
		// 数据库查询出错 (Connection Refused 等) -> 返回 Internal Error
		return nil, status.Errorf(codes.Internal, "failed to query file index: %v", err)
	}

	// 3. Cache Miss (数据库里没查到)
	if idx == nil {
		return &tvrpc.CheckFileResponse{
			Exists: false,
		}, nil
	}

	// 4. 安全兜底：哈希碰撞检测
	// 如果 Hash 一样但 Size 不一样，说明发生碰撞（或者数据库脏数据）
	// 这种情况下我们不敢复用，强制客户端重新上传
	if idx.SizeBytes != req.Size {
		fmt.Printf("⚠️ Hash Collision or Corruption detected! Hash: %s, DB Size: %d, Req Size: %d\n",
			linearHash, idx.SizeBytes, req.Size)
		return &tvrpc.CheckFileResponse{
			Exists: false, // 欺骗客户端说不存在，强制重传
		}, nil
	}

	// 5. 再次确认底层对象存在 (Double Check)
	// 虽然索引表里有记录，但万一 S3 里的对象被误删了呢？
	// 我们做一个快速的 Has 检查，确保万无一失。
	exists, err := s.app.Store.Has(ctx, idx.MerkleRoot)
	if err != nil {
		// S3 报错，安全起见让客户端重传
		return nil, status.Errorf(codes.Internal, "storage check failed: %v", err)
	}
	if !exists {
		fmt.Printf("⚠️ Data Integrity Alert: Index exists for %s but FileNode %s is missing in store.\n",
			linearHash, idx.MerkleRoot)
		// 索引悬空，需要重传
		return &tvrpc.CheckFileResponse{Exists: false}, nil
	}

	// 6. Cache Hit (秒传成功)
	fmt.Printf("⚡ [CheckFile] Instant upload for %s (Hash: %s)\n", linearHash[:8], idx.MerkleRoot[:8])

	// 这里需要处理 optional 字段的赋值
	// proto3 optional 对应 Go 的指针类型 *string
	rootHashStr := idx.MerkleRoot.String()
	return &tvrpc.CheckFileResponse{
		Exists:         true,
		MerkleRootHash: &rootHashStr,
	}, nil
}

// =============================================================================
// 1. Upload (Client-Side Streaming) with Integrity Check & Indexing
// =============================================================================

// Upload 接收客户端的流式上传
// 协议约定：第一帧必须是 Meta (含 sha256)，后续帧是 ChunkData
func (s *DataService) Upload(stream grpc.ClientStreamingServer[tvrpc.UploadRequest, tvrpc.UploadResponse]) error {
	// --- Step 1: 握手阶段 (Handshake) ---
	firstReq, err := stream.Recv()
	if err == io.EOF {
		return status.Error(codes.InvalidArgument, "empty stream: expected metadata frame")
	}
	if err != nil {
		return status.Errorf(codes.Internal, "failed to receive metadata: %v", err)
	}

	meta := firstReq.GetMeta()
	if meta == nil {
		return status.Error(codes.InvalidArgument, "protocol violation: first frame must be FileMeta")
	}

	// 校验 Meta 中的 Linear Hash 是否合法
	clientLinearHash := types.LinearHash(meta.Sha256)
	if !clientLinearHash.IsValid() {
		return status.Errorf(codes.InvalidArgument, "invalid sha256 in metadata: %s", meta.Sha256)
	}

	fmt.Printf("🚀 [Upload] Receiving: %s (Claimed Hash: %s...)\n", meta.Path, clientLinearHash[:8])

	// --- Step 2: 组装阶段 (Wiring) ---
	// 1. gRPC Stream -> io.Reader
	streamReader := NewGrpcStreamReader(stream)

	// 2. 准备 SHA-256 Hasher (用于服务端端计算全量哈希)
	hasher := sha256.New()

	// 3. 组装 TeeReader: 读 streamReader 的同时，自动写入 hasher
	teeReader := io.TeeReader(streamReader, hasher)

	// 4. 创建 Ingester
	ing := ingester.NewIngester(s.app.Store)

	// --- Step 3: 执行阶段 (Execution) ---
	// Ingester 读取 teeReader -> 触发 Hasher 计算 -> 触发 CDC 切分 -> 上传 S3
	ctx := stream.Context()
	fileNode, err := ing.IngestFile(ctx, teeReader)
	if err != nil {
		return status.Errorf(codes.Internal, "ingestion failed: %v", err)
	}

	// --- Step 4: 完整性校验 (Integrity Check) ---
	// 此时流已读完，Hasher 中已经有了全量数据的指纹
	serverLinearHashStr := hex.EncodeToString(hasher.Sum(nil))
	serverLinearHash := types.LinearHash(serverLinearHashStr)

	if serverLinearHash != clientLinearHash {
		// 这是一个严重错误：数据在传输过程中损坏，或者客户端撒谎了
		// 即使 S3 已经存了数据，我们也不能认领它（它是脏数据）
		fmt.Printf("❌ [Upload] Integrity Check Failed!\nClaimed: %s\nActual : %s\n", clientLinearHash, serverLinearHash)
		return status.Errorf(codes.DataLoss, "integrity check failed: data corruption detected")
	}

	// --- Step 5: 建立索引 (Indexing) ---
	// 校验通过，说明 S3 里的数据是完好且正确的。
	// 现在我们将 LinearHash -> MerkleRoot 的关系写入数据库，供下次 CheckFile 使用。
	err = s.app.Repository.SaveFileIndex(ctx, serverLinearHash, fileNode.ID(), fileNode.TotalSize)
	if err != nil {
		// 索引写入失败不应影响上传成功的判定（属于非关键路径失败）
		// 但为了系统健康，我们需要记录日志
		fmt.Printf("⚠️ [Upload] Failed to save file index: %v\n", err)
		// 选择：是报错还是忽略？
		// 架构决策：忽略错误。文件已经安全存入 S3 并返回了 Hash，用户可以继续工作。
		// 只是下次没法“秒传”而已。这是“可用性优先”。
	} else {
		fmt.Printf("✅ [Upload] Index saved. Linear: %s -> Merkle: %s\n", serverLinearHash[:8], fileNode.ID()[:8])
	}

	// --- Step 6: 响应阶段 (Response) ---
	return stream.SendAndClose(&tvrpc.UploadResponse{
		Hash:      fileNode.ID().String(),
		TotalSize: fileNode.TotalSize,
	})
}

// =============================================================================
// 2. Download (Server-Side Streaming)
// =============================================================================

// Download 处理下载请求
func (s *DataService) Download(req *tvrpc.DownloadRequest, stream grpc.ServerStreamingServer[tvrpc.DownloadResponse]) error {
	// --- Step 1: 参数校验 ---
	ctx := stream.Context()
	inputHash := req.Hash

	// [修改] 智能哈希解析：支持完整哈希和短哈希
	var targetHash types.Hash

	if len(inputHash) == 64 {
		// 1. 如果是完整哈希，直接使用 (性能最优)
		targetHash = types.Hash(inputHash)
	} else {
		// 2. 如果是短哈希，尝试扩展 (用户友好)
		// 注意：ExpandHash 是 Store 接口的一部分，我们在 Phase 1 已经实现了
		fullHash, err := s.app.Store.ExpandHash(ctx, types.HashPrefix(inputHash))
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return status.Errorf(codes.NotFound, "hash prefix %s not found", inputHash)
			}
			if errors.Is(err, storage.ErrAmbiguousHash) {
				return status.Errorf(codes.InvalidArgument, "hash prefix %s is ambiguous", inputHash)
			}
			return status.Errorf(codes.Internal, "hash expansion failed: %v", err)
		}
		targetHash = fullHash
	}

	fmt.Printf("📦 [Download] Serving: %s (Expanded from: %s)\n", targetHash, inputHash)

	// --- Step 2: 组装适配器 ---
	// 把 gRPC stream 伪装成 io.Writer
	streamWriter := NewGrpcStreamWriter(stream)

	// --- Step 3: 执行导出 ---
	// 创建 Exporter
	exp := s.app.GetExporter() // 稍后要在 App 里加这个 helper 方法，或者直接 new

	// 调用核心逻辑
	// 注意：Exporter 内部会检测 streamWriter 是否支持 WriteAt。
	// 显然 GrpcStreamWriter 不支持，所以 Exporter 会自动降级为串行流式传输，
	// 这正是 gRPC Server Streaming 所需要的模式。
	err := exp.ExportFile(stream.Context(), targetHash, streamWriter)

	// --- Step 4: 错误处理 ---
	if err != nil {
		// 映射核心层错误到 gRPC 状态码
		if errors.Is(err, storage.ErrNotFound) {
			return status.Errorf(codes.NotFound, "object %s not found", targetHash)
		}
		return status.Errorf(codes.Internal, "export failed: %v", err)
	}

	return nil
}
