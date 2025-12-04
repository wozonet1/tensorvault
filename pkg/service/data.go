package service

import (
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
// 1. Upload (Client-Side Streaming)
// =============================================================================

// Upload 接收客户端的流式上传
// 协议约定：第一帧必须是 Meta，后续帧是 ChunkData
func (s *DataService) Upload(stream grpc.ClientStreamingServer[tvrpc.UploadRequest, tvrpc.UploadResponse]) error {
	// --- Step 1: 握手阶段 (Handshake) ---
	// 我们必须手动读取第一条消息，以确保它包含元数据
	firstReq, err := stream.Recv()
	if err == io.EOF {
		return status.Error(codes.InvalidArgument, "empty stream: expected metadata frame")
	}
	if err != nil {
		return status.Errorf(codes.Internal, "failed to receive metadata: %v", err)
	}

	// 验证第一帧是否为 Meta
	// 这里用到了生成代码里的 GetMeta()，它会自动检查 Payload 类型
	meta := firstReq.GetMeta()
	if meta == nil {
		return status.Error(codes.InvalidArgument, "protocol violation: first frame must be FileMeta")
	}

	// (可选) 这里可以记录日志，比如 "Receiving file: meta.Path"
	fmt.Printf("🚀 [Upload] Receiving: %s\n", meta.Path)

	// --- Step 2: 组装阶段 (Wiring) ---
	// 使用我们写的适配器，把剩余的 gRPC 流伪装成 io.Reader
	// 注意：stream 已经被读取了一次，后续 Recv 会自动读下一帧
	streamReader := NewGrpcStreamReader(stream)

	// 创建 Ingester
	// 注意：复用 app.Store，这使得所有上传自动享受 Redis 缓存和 S3 存储能力
	ing := ingester.NewIngester(s.app.Store)

	// --- Step 3: 执行阶段 (Execution) ---
	// 调用核心逻辑。Ingester 会不断从 streamReader 读取，直到 io.EOF
	ctx := stream.Context() // 获取上下文以处理取消
	fileNode, err := ing.IngestFile(ctx, streamReader)
	if err != nil {
		return status.Errorf(codes.Internal, "ingestion failed: %v", err)
	}

	// --- Step 4: 响应阶段 (Response) ---
	// 发送唯一的响应包并关闭流
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
	// 我们之前在 Proto 里加了 buf.validate，所以这里 req 应该是合法的
	// 但为了保险，可以再次校验 Hash 格式
	hash := types.Hash(req.Hash)
	if !hash.IsValid() {
		return status.Errorf(codes.InvalidArgument, "invalid hash format")
	}

	fmt.Printf("📦 [Download] Serving: %s\n", hash)

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
	err := exp.ExportFile(stream.Context(), hash, streamWriter)

	// --- Step 4: 错误处理 ---
	if err != nil {
		// 映射核心层错误到 gRPC 状态码
		if errors.Is(err, storage.ErrNotFound) {
			return status.Errorf(codes.NotFound, "object %s not found", hash)
		}
		return status.Errorf(codes.Internal, "export failed: %v", err)
	}

	return nil
}
