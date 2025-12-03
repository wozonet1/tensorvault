package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/app"
	"tensorvault/pkg/core"
	"tensorvault/pkg/index"
	"tensorvault/pkg/meta"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/storage/disk"
	"tensorvault/pkg/types"
)

// setupTestService 构建一个隔离的 MetaService 测试环境
func setupTestService(t *testing.T) (*MetaService, *app.App) {
	tmpDir := t.TempDir()

	// 1. 初始化存储
	storePath := filepath.Join(tmpDir, "objects")
	require.NoError(t, os.MkdirAll(storePath, 0755))
	store, err := disk.NewAdapter(storePath)
	require.NoError(t, err)

	// 2. 初始化索引 (虽然 MetaService 不直接用 Index，但 App 需要)
	idxPath := filepath.Join(tmpDir, "index.json")
	require.NoError(t, os.WriteFile(idxPath, []byte("{}"), 0644))
	idx, err := index.NewIndex(idxPath)
	require.NoError(t, err)

	// 3. 初始化内存数据库
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	metaDB := meta.NewWithConn(db)
	require.NoError(t, metaDB.AutoMigrate(&meta.Ref{}, &meta.CommitModel{}))

	repo := meta.NewRepository(metaDB)
	refMgr := refs.NewManager(repo)

	// 4. 组装 App
	application := &app.App{
		Store:      store,
		Index:      idx,
		Refs:       refMgr,
		RepoPath:   tmpDir,
		Repository: repo,
	}

	return NewMetaService(application), application
}

// 辅助函数：生成合法 Hash
func mockHash(input string) string {
	return core.CalculateBlobHash([]byte(input)).String()
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
		TreeHash:   treeHash,
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
