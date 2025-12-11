package meta

import (
	"context"
	"fmt"
	"testing"

	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestRepo 构建隔离的测试环境
func setupTestRepo(t *testing.T) *Repository {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	metaDB := NewWithConn(db)
	require.NoError(t, metaDB.AutoMigrate(&Ref{}, &CommitModel{}, &FileIndex{}))

	return NewRepository(metaDB)
}

// -----------------------------------------------------------------------------
// 测试用例
// -----------------------------------------------------------------------------

func TestRepository_CommitLifecycle(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// 1. 准备数据
	treeHash := mockHash("tree_data")
	parentHash := mockHash("parent_data")

	// 使用 Helper，代码极其简洁
	commitObj := mustNewCommit(t, treeHash, []types.Hash{parentHash}, "Alice", "Init", "Failed to create commit")

	// 2. 写入
	mustIndexCommit(t, repo, commitObj, "First index should succeed")

	// 3. 读取并验证
	storedCommit, err := repo.GetCommit(ctx, commitObj.ID())
	require.NoError(t, err)

	assert.Equal(t, commitObj.ID(), storedCommit.Hash)
	assert.Equal(t, "Alice", storedCommit.Author)

	// 验证 JSONB 存储
	expectedJSON := fmt.Sprintf(`["%s"]`, parentHash)
	assert.JSONEq(t, expectedJSON, string(storedCommit.Parents))
}

func TestRepository_IndexCommit_Idempotency(t *testing.T) {
	repo := setupTestRepo(t)
	commitObj := mustNewCommit(t, mockHash("tree"), nil, "Bob", "Update")

	// 1. 写入两次
	mustIndexCommit(t, repo, commitObj, "1st write failed")
	mustIndexCommit(t, repo, commitObj, "2nd write (idempotency check) failed")

	// 2. 验证数据库中只有一条记录 (副作用检查)
	var count int64
	err := repo.db.GetConn().Model(&CommitModel{}).Where("hash = ?", commitObj.ID()).Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "Should have exactly 1 record after duplicate inserts")
}

func TestRepository_FindCommitsByAuthor(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// 1. 准备数据 (手动控制时间戳以保证排序确定性)
	c1 := mustNewCommit(t, mockHash("t1"), nil, "Alice", "1")
	c1.Timestamp = 1000

	c2 := mustNewCommit(t, mockHash("t2"), nil, "Bob", "2")

	c3 := mustNewCommit(t, mockHash("t3"), nil, "Alice", "3")
	c3.Timestamp = 3000 // 最新

	// 2. 写入
	mustIndexCommit(t, repo, c1)
	mustIndexCommit(t, repo, c2)
	mustIndexCommit(t, repo, c3)

	// 3. 查询
	results, err := repo.FindCommitsByAuthor(ctx, "Alice", 10)
	require.NoError(t, err)

	assert.Equal(t, 2, len(results))
	// 验证排序：最新的在前 (ORDER BY timestamp DESC)
	assert.Equal(t, c3.ID(), results[0].Hash, "Newest commit should be first")
	assert.Equal(t, c1.ID(), results[1].Hash, "Oldest commit should be last")
}

func TestRepository_Ref_CAS(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background() // Unhappy path 需要用到 ctx

	refName := "HEAD"
	hashV1 := mockHash("v1")
	hashV2 := mockHash("v2")

	// 1. 首次创建 (Happy Path)
	mustUpdateRef(t, repo, refName, hashV1, 0, "Initial creation failed")

	// 验证状态
	ref, err := repo.GetRef(ctx, refName)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ref.Version)

	// 2. 模拟并发冲突 (Unhappy Path)
	// 这里不能用 mustUpdateRef，因为我们要断言它 *失败*
	wrongVersion := int64(999)
	err = repo.UpdateRef(ctx, refName, hashV2, wrongVersion)
	assert.ErrorIs(t, err, ErrConcurrentUpdate, "Should fail when version mismatches")

	// 3. 正常更新 (Happy Path)
	// 使用正确的版本号 1
	mustUpdateRef(t, repo, refName, hashV2, 1, "Valid update failed")

	// 验证版本号自增
	ref, err = repo.GetRef(ctx, refName)
	require.NoError(t, err)
	assert.Equal(t, int64(2), ref.Version)
	assert.Equal(t, hashV2, ref.CommitHash)
}

func TestRepository_Ref_ConcurrentCreate(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()
	refName := "HEAD"

	// 1. 用户 A 抢先创建成功
	mustUpdateRef(t, repo, refName, mockHash("A"), 0)

	// 2. 用户 B 晚了一步，但也以为 oldVersion 是 0
	// 这里预期失败，所以用原生调用
	err := repo.UpdateRef(ctx, refName, mockHash("B"), 0)

	// 3. 断言：应该返回统一的 CAS 错误 (得益于我们在 UpdateRef 里的兼容性修复)
	assert.ErrorIs(t, err, ErrConcurrentUpdate, "Concurrent creation should return CAS error")
}

func TestRepository_FileIndex_Flow(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	linearHash := types.LinearHash(mockHash("full_content_sha256"))
	merkleRoot := mockHash("dag_root")
	size := int64(1024)

	// 1. Cache Miss
	got, err := repo.GetFileIndex(ctx, linearHash)
	require.NoError(t, err)
	assert.Nil(t, got, "Should return nil on cache miss")

	// 2. Save (First time)
	err = repo.SaveFileIndex(ctx, linearHash, merkleRoot, size)
	require.NoError(t, err)

	// 3. Cache Hit
	got, err = repo.GetFileIndex(ctx, linearHash)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, merkleRoot, got.MerkleRoot)
	assert.Equal(t, size, got.SizeBytes)

	// 4. Save (Duplicate/Idempotency)
	// 尝试写入一个不同的 root (模拟并发冲突或恶意覆写)，应该被忽略
	otherRoot := mockHash("other_root")
	err = repo.SaveFileIndex(ctx, linearHash, otherRoot, size)
	require.NoError(t, err)

	// 验证数据未被修改 (First write wins)
	got, err = repo.GetFileIndex(ctx, linearHash)
	require.NoError(t, err)
	assert.Equal(t, merkleRoot, got.MerkleRoot, "Existing index should be immutable")
}
