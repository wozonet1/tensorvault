package refs

import (
	"context"
	"testing"

	"tensorvault/pkg/meta"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TODO: read
// setupTestEnv 搭建基于内存 SQLite 的测试环境
func setupTestEnv(t *testing.T) *Manager {
	// 1. 初始化内存 SQLite
	// "file::memory:?cache=shared" 确保连接池共享同一个内存实例
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // 测试时静默日志
	})
	require.NoError(t, err)

	// 2. 自动迁移表结构 (Ref 表)
	err = db.AutoMigrate(&meta.Ref{})
	require.NoError(t, err)

	// 3. 组装依赖
	// 使用我们在 meta 包新加的辅助函数注入 DB
	metaDB := meta.NewTestDB(db)
	repo := meta.NewRepository(metaDB)
	mgr := NewManager(repo)

	return mgr
}
func TestRefFlow_Lifecycle(t *testing.T) {
	mgr := setupTestEnv(t)
	ctx := context.Background()

	// 1. 初始状态应该是 NoHead
	_, _, err := mgr.GetHead(ctx)
	assert.ErrorIs(t, err, ErrNoHead, "空仓库应该返回 ErrNoHead")

	// 2. 第一次提交 (Initial Commit)
	// oldVersion 传 0
	hash1 := "hash_v1_111111111111111111111111111111111111111111111111111111111111"
	err = mgr.UpdateHead(ctx, hash1, 0)
	require.NoError(t, err, "首次 UpdateHead 应该成功")

	// 3. 验证读取
	gotHash, gotVer, err := mgr.GetHead(ctx)
	require.NoError(t, err)
	assert.Equal(t, hash1, gotHash)
	assert.Equal(t, int64(1), gotVer, "第一次版本号应该是 1")

	// 4. 第二次提交 (Normal Update)
	// 基于版本 1 更新
	hash2 := "hash_v2_222222222222222222222222222222222222222222222222222222222222"
	err = mgr.UpdateHead(ctx, hash2, 1)
	require.NoError(t, err, "基于正确版本的更新应该成功")

	// 验证
	gotHash, gotVer, err = mgr.GetHead(ctx)
	require.NoError(t, err)
	assert.Equal(t, hash2, gotHash)
	assert.Equal(t, int64(2), gotVer, "版本号应该递增为 2")
}
func TestRefFlow_OptimisticLocking(t *testing.T) {
	mgr := setupTestEnv(t)
	ctx := context.Background()

	// 1. 初始化到版本 1
	hash1 := "hash_v1"
	require.NoError(t, mgr.UpdateHead(ctx, hash1, 0))

	// 2. 获取当前状态 (Version = 1)
	_, ver, err := mgr.GetHead(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ver)

	// 3. 模拟并发场景：
	// 用户 A 试图基于版本 1 更新到 hash_A
	// 用户 B 抢先一步基于版本 1 更新到了 hash_B

	// 用户 B 先成功了
	hashB := "hash_user_B"
	err = mgr.UpdateHead(ctx, hashB, ver)
	require.NoError(t, err, "用户 B 应该更新成功")

	// 现在数据库里的版本应该是 2

	// 用户 A 姗姗来迟，拿着过期的 ver=1 试图更新
	hashA := "hash_user_A"
	err = mgr.UpdateHead(ctx, hashA, ver) // 这里的 ver 还是 1

	// 4. 验证 CAS 失败
	assert.ErrorIs(t, err, ErrStaleHead, "使用过期的版本号更新应该被拒绝")

	// 5. 确保数据没有被覆盖
	currHash, currVer, _ := mgr.GetHead(ctx)
	assert.Equal(t, hashB, currHash, "HEAD 应该保持为用户 B 的值")
	assert.Equal(t, int64(2), currVer)
}
