package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/core"
	"tensorvault/pkg/types"
)

// setupTestService 构建一个隔离的 MetaService 测试环境
func setupTestService(t *testing.T) (*MetaService, *app.App) {
	app := setupTestApp(t)

	return NewMetaService(app), app
}

func TestMetaService_GetHead(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Case 1: 空仓库
	resp, err := svc.GetHead(ctx, &tvrpc.GetHeadRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Exists)
	assert.Equal(t, int64(0), resp.Version)

	// Case 2: 有数据 (手动插入模拟)
	// 为了简单，我们直接操作底层 Ref Manager
	fakeHash := types.Hash(mockHash("init"))
	require.NoError(t, svc.app.Refs.UpdateRef(ctx, "HEAD", fakeHash, 0))

	resp, err = svc.GetHead(ctx, &tvrpc.GetHeadRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Exists)
	assert.Equal(t, fakeHash.String(), resp.Hash)
	assert.Equal(t, int64(1), resp.Version)
}

func TestMetaService_Commit_HappyPath(t *testing.T) {
	svc, app := setupTestService(t)
	ctx := context.Background()

	treeHash := mockHash("root_tree")
	req := &tvrpc.CommitRequest{
		Message:    "First Commit",
		Author:     "Tester",
		TreeHash:   treeHash.String(),
		BranchName: "main", // 指定分支
	}

	// 执行提交
	resp, err := svc.Commit(ctx, req)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.CommitHash)

	// 验证副作用
	// 1. 检查 Ref 是否更新
	refHash, ver, err := app.Refs.GetRef(ctx, "main")
	require.NoError(t, err)
	assert.Equal(t, resp.CommitHash, refHash.String())
	assert.Equal(t, int64(1), ver)

	// 2. 检查 DB 是否有记录
	commitModel, err := app.Repository.GetCommit(ctx, types.Hash(resp.CommitHash))
	require.NoError(t, err)
	assert.Equal(t, "First Commit", commitModel.Message)
}

func TestMetaService_Commit_Validation(t *testing.T) {
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Case: Invalid Hash Length
	req := &tvrpc.CommitRequest{
		Message:  "Bad Request",
		TreeHash: "short_hash", // < 64 chars
	}

	_, err := svc.Commit(ctx, req)
	require.Error(t, err)

	// 验证错误码是否为 InvalidArgument
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}
func TestMetaService_BuildTree(t *testing.T) {
	svc, app := setupTestService(t)
	ctx := context.Background()

	// 1. 准备数据: 创建一个 FileNode
	// 假设这个文件大小是 100 字节
	fileNode, err := core.NewFileNode(100, nil)
	require.NoError(t, err)

	// 2. [关键修复] 模拟 Upload 的完整行为：
	//    不仅要存对象到 S3 (Store)，还要存索引到 SQL (Repository)
	//    因为 BuildTree 强依赖 SQL 中的 Size 记录

	// 2.a 存入对象存储 (物理数据)
	require.NoError(t, app.Store.Put(ctx, fileNode))

	// 2.b 存入元数据索引 (逻辑数据)
	// 我们需要伪造一个 LinearHash。
	// 在 BuildTree 逻辑中，它通过 MerkleRoot 反查 Size，并不关心 LinearHash 是什么，
	// 但数据库约束要求 LinearHash 必须存在且合法。
	fakeLinearHash := types.LinearHash(mockHash("fake_content_for_test").String())

	// 写入索引：建立 LinearHash -> MerkleRoot + Size 的映射
	err = app.Repository.SaveFileIndex(ctx, fakeLinearHash, fileNode.ID(), fileNode.TotalSize)
	require.NoError(t, err, "Failed to seed file index")

	// 3. 调用 BuildTree
	req := &tvrpc.BuildTreeRequest{
		FileMap: map[string]string{
			"data/train.csv": fileNode.ID().String(),
		},
	}
	resp, err := svc.BuildTree(ctx, req)
	require.NoError(t, err)

	// 4. 验证
	assert.NotEmpty(t, resp.TreeHash)

	// (可选) 进一步验证：确保生成的 Tree 对象真的写入了 Store
	exists, err := app.Store.Has(ctx, types.Hash(resp.TreeHash))
	require.NoError(t, err)
	assert.True(t, exists, "Root tree object should be persisted")
}
