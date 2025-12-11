package service

import (
	"context"
	"errors"
	"fmt"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/core"
	"tensorvault/pkg/index"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/treebuilder"
	"tensorvault/pkg/types"

	"buf.build/go/protovalidate"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MetaService struct {
	tvrpc.UnimplementedMetaServiceServer
	app       *app.App
	validator protovalidate.Validator
}

func NewMetaService(application *app.App) *MetaService {
	v, err := protovalidate.New()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize validator: %v", err))
	}
	return &MetaService{
		app:       application,
		validator: v,
	}
}

// GetHead 获取当前分支的 HEAD
func (s *MetaService) GetHead(ctx context.Context, req *tvrpc.GetHeadRequest) (*tvrpc.GetHeadResponse, error) {
	// 虽然 req 是空的，但校验是个好习惯
	if err := s.validator.Validate(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	hash, ver, err := s.app.Refs.GetHead(ctx)
	if err != nil {
		if errors.Is(err, refs.ErrNoHead) {
			return &tvrpc.GetHeadResponse{
				Exists:  false,
				Hash:    nil,
				Version: 0,
			}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to read HEAD: %v", err)
	}
	hashStr := hash.String()
	return &tvrpc.GetHeadResponse{
		Exists:  true,
		Hash:    &hashStr,
		Version: ver,
	}, nil
}

// GetRef 获取指定引用的当前状态
// 用于解析 "refs/heads/main" 或 "datasets/bindingdb" 等引用
func (s *MetaService) GetRef(ctx context.Context, req *tvrpc.GetRefRequest) (*tvrpc.GetRefResponse, error) {
	// 1. 参数校验
	if err := s.validator.Validate(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// 2. 查询引用逻辑
	// 注意：s.app.Refs.GetRef 在底层遇到 ErrRefNotFound 时，会返回空 hash 和 nil error
	// 这是我们在 pkg/refs/manager.go 中定义的行为
	hash, ver, err := s.app.Refs.GetRef(ctx, req.Name)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to resolve ref %s: %v", req.Name, err)
	}

	// 3. 处理不存在的情况
	if hash == "" {
		return &tvrpc.GetRefResponse{
			Exists:  false,
			Hash:    nil, // optional 字段设为 nil
			Version: 0,
		}, nil
	}

	// 4. 返回存在的引用
	hashStr := hash.String()
	return &tvrpc.GetRefResponse{
		Exists:  true,
		Hash:    &hashStr, // 取地址赋值给 optional string
		Version: ver,
	}, nil
}

// Commit 处理提交请求
func (s *MetaService) Commit(ctx context.Context, req *tvrpc.CommitRequest) (*tvrpc.CommitResponse, error) {
	// 1. Runtime Validation
	if err := s.validator.Validate(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// 2. DTO -> Domain Object
	treeHash := types.Hash(req.TreeHash)
	var parents []types.Hash
	for _, p := range req.ParentHashes {
		parents = append(parents, types.Hash(p))
	}

	// 3. Create Commit (Immutable)
	commitObj, err := core.NewCommit(treeHash, parents, req.Author, req.Message)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create commit object: %v", err)
	}

	// 4. Persist Payload (S3/Disk)
	if err := s.app.Store.Put(ctx, commitObj); err != nil {
		return nil, status.Errorf(codes.Internal, "storage backend error: %v", err)
	}

	// 5. Index Metadata (DB)
	if err := s.app.Repository.IndexCommit(ctx, commitObj); err != nil {
		return nil, status.Errorf(codes.Internal, "metadata indexing error: %v", err)
	}

	// 6. Update Reference (CAS)
	targetBranch := req.BranchName
	if targetBranch == "" {
		targetBranch = "HEAD"
	}

	// Get current version for Optimistic Locking
	_, currentVer, err := s.app.Refs.GetRef(ctx, targetBranch)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to resolve ref %s: %v", targetBranch, err)
	}

	// Atomic Update
	if err := s.app.Refs.UpdateRef(ctx, targetBranch, commitObj.ID(), currentVer); err != nil {
		return nil, status.Errorf(codes.Aborted, "concurrent update detected on %s: %v", targetBranch, err)
	}

	fmt.Printf("✅ [Server] New Commit: %s -> %s (Author: %s)\n", targetBranch, commitObj.ID(), req.Author)

	return &tvrpc.CommitResponse{
		CommitHash: commitObj.ID().String(),
	}, nil
}

// BuildTree 接收文件清单，构建 Merkle Tree
func (s *MetaService) BuildTree(ctx context.Context, req *tvrpc.BuildTreeRequest) (*tvrpc.BuildTreeResponse, error) {
	// 1. 基础校验
	if err := s.validator.Validate(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	fmt.Printf("🏗️ [BuildTree] Building tree from %d files...\n", len(req.FileMap))

	// 2. 构建内存索引 (Transient Index)
	// 我们复用 index.Index 结构，但手动初始化，不绑定磁盘文件
	tempIndex := &index.Index{
		Entries: make(map[string]index.Entry),
	}
	var hashes []types.Hash
	for _, h := range req.FileMap {
		hashes = append(hashes, types.Hash(h))
	}
	// 3. 填充索引并校验存在性
	sizeMap, err := s.app.Repository.GetSizesByMerkleRoots(ctx, hashes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to query sizes: %v", err)
	}
	for path, hashStr := range req.FileMap {
		size, found := sizeMap[hashStr]

		// [兜底策略] 如果 SQL 里没查到（可能索引丢失，或者直接调 Upload 没写索引）
		if !found {
			// Option A: 报错 (严格模式)
			// return nil, status.Errorf(codes.DataLoss, "metadata missing for hash %s", hashStr)

			// Option B: 查 S3 (高可用模式 - 推荐)
			// objInfo, err := s.app.Store.Stat(hashStr) ...
			// size = objInfo.Size

			// 这里为了 MVP 简单，先报错提示
			return nil, status.Errorf(codes.NotFound, "size metadata not found for %s", hashStr)
		}

		// 添加到临时索引
		tempIndex.Add(path, types.Hash(hashStr), size)
	}

	// 4. 执行构建 (Heavy Lifting)
	// 复用 treebuilder，它会自动处理目录层级拆分、排序、Hash计算和持久化
	builder := treebuilder.NewBuilder(s.app.Store)
	rootHash, err := builder.Build(ctx, tempIndex)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build merkle tree: %v", err)
	}

	fmt.Printf("✅ [BuildTree] Success. Root: %s\n", rootHash)

	return &tvrpc.BuildTreeResponse{
		TreeHash: rootHash.String(),
	}, nil
}
