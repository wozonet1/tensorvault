package service

import (
	"context"
	"errors"
	"fmt"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/core"
	"tensorvault/pkg/refs"
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
