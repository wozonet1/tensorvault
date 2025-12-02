package meta

import (
	"context"
	"fmt"
	"testing"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestRepo 构建一个独立的、基于内存 SQLite 的测试环境
func setupTestRepo(t *testing.T) *Repository {
	// 1. 使用 t.Name() 隔离不同测试用例的数据库实例
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())

	// 2. 连接 SQLite
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // 静默日志，保持测试输出干净
	})
	require.NoError(t, err)

	// 3. 注入连接 (假设你已经把 NewTestDB 改名为 NewFromConn)
	metaDB := NewWithConn(db)

	// 4. 自动迁移 Schema
	err = metaDB.AutoMigrate(&Ref{}, &CommitModel{})
	require.NoError(t, err)

	return NewRepository(metaDB)
}

// TestRepository_CommitLifecycle 测试 Commit 的完整生命周期：索引 -> 读取
func TestRepository_CommitLifecycle(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// 1. 构造一个模拟的 core.Commit 对象
	// 注意：我们需要构造一个合法的 Commit，这里简化构造过程
	parents := []types.Hash{"parent_hash_123"}
	commitObj, err := core.NewCommit("tree_hash_abc", parents, "Alice", "Initial Commit")
	require.NoError(t, err)

	// 2. 测试写入 (Index)
	err = repo.IndexCommit(ctx, commitObj)
	assert.NoError(t, err, "IndexCommit should succeed")

	// 3. 测试读取 (Get)
	storedCommit, err := repo.GetCommit(ctx, commitObj.ID())
	assert.NoError(t, err, "GetCommit should succeed")

	// 4. 验证字段一致性
	assert.Equal(t, commitObj.ID(), storedCommit.Hash)
	assert.Equal(t, "Alice", storedCommit.Author)
	assert.Equal(t, "Initial Commit", storedCommit.Message)
	assert.Equal(t, commitObj.Timestamp, storedCommit.Timestamp)

	// 验证 Parents (JSONB 序列化是否正确)
	// GORM/SQLite 会将其存为字符串，我们需要确认它不是空的
	assert.JSONEq(t, `["parent_hash_123"]`, string(storedCommit.Parents), "Parents JSON should match")
}

// TestRepository_IndexCommit_Idempotency 测试 Commit 索引的幂等性
// 即：重复索引同一个 Commit 不应报错
func TestRepository_IndexCommit_Idempotency(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	commitObj, _ := core.NewCommit("tree", nil, "Bob", "Update")

	// 第一次写入
	err := repo.IndexCommit(ctx, commitObj)
	assert.NoError(t, err)

	// 第二次写入 (完全相同的 Hash)
	err = repo.IndexCommit(ctx, commitObj)
	assert.NoError(t, err, "Duplicate IndexCommit should be ignored (Idempotent)")
}

// TestRepository_FindCommitsByAuthor 测试按作者查询功能
func TestRepository_FindCommitsByAuthor(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// 插入 3 条数据
	c1, _ := core.NewCommit("t1", nil, "Alice", "1")
	c2, _ := core.NewCommit("t2", nil, "Bob", "2")
	time.Sleep(10 * time.Millisecond)                // 确保时间戳不同
	c3, _ := core.NewCommit("t3", nil, "Alice", "3") // Alice 的第二条

	require.NoError(t, repo.IndexCommit(ctx, c1))
	require.NoError(t, repo.IndexCommit(ctx, c2))
	require.NoError(t, repo.IndexCommit(ctx, c3))

	// 查询 Alice
	results, err := repo.FindCommitsByAuthor(ctx, "Alice", 10)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results), "Alice should have 2 commits")

	// 验证排序 (应该按时间倒序，最新的 c3 在前)
	assert.Equal(t, c3.ID(), results[0].Hash)
	assert.Equal(t, c1.ID(), results[1].Hash)
}

// TestRepository_Ref_CAS 测试引用的乐观锁逻辑
func TestRepository_Ref_CAS(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	refName := "HEAD"
	hashV1 := "hash_v1"
	hashV2 := "hash_v2"

	// 1. 首次创建 (OldVersion = 0)
	err := repo.UpdateRef(ctx, refName, hashV1, 0)
	assert.NoError(t, err)

	// 2. 获取当前版本
	ref, err := repo.GetRef(ctx, refName)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ref.Version)

	// 3. 模拟并发冲突：使用错误的 OldVersion (0) 更新
	err = repo.UpdateRef(ctx, refName, hashV2, 0)
	assert.ErrorIs(t, err, ErrConcurrentUpdate, "Should fail with stale version")

	// 4. 正常更新：使用正确的 OldVersion (1)
	err = repo.UpdateRef(ctx, refName, hashV2, 1)
	assert.NoError(t, err)

	// 5. 验证版本号自增
	ref, _ = repo.GetRef(ctx, refName)
	assert.Equal(t, int64(2), ref.Version)
	assert.Equal(t, hashV2, ref.CommitHash)
}
