package service

import (
	"context"
	"errors"
	"fmt"

	"buf.build/go/protovalidate"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/core"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MetaService struct {
	tvrpc.UnimplementedMetaServiceServer
	app       *app.App
	validator protovalidate.Validator
}

func NewMetaService(application *app.App) *MetaService {
	// 初始化校验器 (这是一个高开销操作，做一次就行)
	v, err := protovalidate.New()
	if err != nil {
		// 如果校验器初始化失败（通常是 Proto 定义有逻辑矛盾），服务不应启动
		panic(fmt.Sprintf("failed to initialize validator: %v", err))
	}

	return &MetaService{
		app:       application,
		validator: v,
	}
}

// GetHead 处理获取当前分支 HEAD 的请求
func (s *MetaService) GetHead(ctx context.Context, req *tvrpc.GetHeadRequest) (*tvrpc.GetHeadResponse, error) {
	// 1. 校验请求 (虽然 GetHeadRequest 目前为空，但为了统一规范建议加上)
	if err := s.validator.Validate(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	hash, ver, err := s.app.Refs.GetHead(ctx)
	if err != nil {
		if errors.Is(err, refs.ErrNoHead) {
			return &tvrpc.GetHeadResponse{
				Exists:  false,
				Hash:    "",
				Version: 0,
			}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to read HEAD: %v", err)
	}

	return &tvrpc.GetHeadResponse{
		Exists:  true,
		Hash:    hash.String(),
		Version: ver,
	}, nil
}

// Commit 处理提交请求
func (s *MetaService) Commit(ctx context.Context, req *tvrpc.CommitRequest) (*tvrpc.CommitResponse, error) {
	// A. 统一校验 (One-Liner Validation)
	// 这一行代码替代了之前所有的 if len != 64 ...
	// 如果校验失败，err 会包含非常有用的信息，例如: "tree_hash: length must be 64"
	if err := s.validator.Validate(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// B. 数据转换 (DTO -> Domain Object)
	// 此时我们已经确信 req.ParentHashes 里的字符串都是合法的 Hash，可以放心强转
	var parents []types.Hash
	for _, p := range req.ParentHashes {
		parents = append(parents, types.Hash(p))
	}

	// C. 核心逻辑：构造 Commit 对象
	commitObj, err := core.NewCommit(
		types.Hash(req.TreeHash), // 也是安全的
		parents,
		req.Author,
		req.Message,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create commit object: %v", err)
	}

	// D. 编排 (Orchestration)
	// 1. 持久化 (S3)
	if err := s.app.Store.Put(ctx, commitObj); err != nil {
		return nil, status.Errorf(codes.Internal, "storage backend failed: %v", err)
	}

	// 2. 索引元数据 (Postgres)
	if err := s.app.Repository.IndexCommit(ctx, commitObj); err != nil {
		return nil, status.Errorf(codes.Internal, "metadata indexing failed: %v", err)
	}

	// 3. 更新引用 (CAS)
	// 获取当前 HEAD (为了 CAS)
	_, currentVer, err := s.app.Refs.GetHead(ctx)
	if err != nil && !errors.Is(err, refs.ErrNoHead) {
		return nil, status.Errorf(codes.Internal, "failed to check current HEAD: %v", err)
	}

	// 尝试原子更新
	if err := s.app.Refs.UpdateHead(ctx, commitObj.ID(), currentVer); err != nil {
		return nil, status.Errorf(codes.Aborted, "concurrent update detected (CAS failed): %v", err)
	}

	fmt.Printf("✅ [Server] New Commit: %s (Author: %s)\n", commitObj.ID(), req.Author)

	return &tvrpc.CommitResponse{
		CommitHash: commitObj.ID().String(),
	}, nil
}
