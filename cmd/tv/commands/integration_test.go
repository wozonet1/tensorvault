package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"tensorvault/pkg/app"
	"tensorvault/pkg/index"
	"tensorvault/pkg/meta"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/storage/disk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupIntegrationEnv 搭建一个使用 真实文件系统 + 内存数据库 的集成环境
func setupIntegrationEnv(t *testing.T) (*app.App, string) {
	// 1. 准备临时工作目录
	tmpDir := t.TempDir()

	// 2. 初始化 .tv 目录结构
	tvDir := filepath.Join(tmpDir, ".tv")
	objectsDir := filepath.Join(tvDir, "objects")
	require.NoError(t, os.MkdirAll(objectsDir, 0755))

	// 3. 初始化真实的文件存储 (DiskStore)
	store, err := disk.NewAdapter(objectsDir)
	require.NoError(t, err)

	// 4. 初始化 Index
	idx, err := index.NewIndex(filepath.Join(tvDir, "index.json"))
	require.NoError(t, err)

	// 5. 初始化 内存数据库 (模拟 Postgres)
	// 关键：使用内存 SQLite 代替 Postgres，保证测试极速运行且无外部依赖
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// 迁移表结构
	metaDB := meta.NewWithConn(db) // 使用我们刚加的 NewFromConn
	require.NoError(t, metaDB.AutoMigrate(&meta.Ref{}, &meta.CommitModel{}))

	repo := meta.NewRepository(metaDB)
	refMgr := refs.NewManager(repo)

	// 6. 组装 App
	application := &app.App{
		Store:      store,
		Index:      idx,
		Refs:       refMgr,
		RepoPath:   tvDir,
		Repository: repo,
	}

	// 7. 【关键】注入全局变量 TV
	// 因为 cmd 包依赖全局变量 TV，我们在测试里临时覆盖它
	TV = application

	return application, tmpDir
}

func TestIntegration_CommitFlow(t *testing.T) {
	// 1. 搭建环境
	app, tmpDir := setupIntegrationEnv(t)
	ctx := context.Background()

	// 2. 模拟用户操作：创建一个文件并添加到 Index
	// echo "hello world" > data.txt
	testFile := filepath.Join(tmpDir, "data.txt")
	err := os.WriteFile(testFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	// tv add data.txt
	// 这里我们直接操作 Index API，模拟 add 的效果 (也可以直接调 addCmd.RunE)
	// 为了聚焦测试 Commit，我们假设 Add 已经成功
	fileHash := "5eb63bbbe01eeed093cb22bb8f5acdc3" // md5("hello world") 假装是这个
	app.Index.Add("data.txt", fileHash, 11)

	// 3. 执行 Commit 命令
	// 模拟参数：tv commit -m "First Commit"
	commitMsg = "Integration Test Commit" // 设置全局 flag 变量
	err = commitCmd.RunE(commitCmd, []string{})
	require.NoError(t, err, "Commit command should succeed")

	// --- 验证阶段 (The Verification) ---

	// A. 验证 HEAD 是否更新
	headHash, ver, err := app.Refs.GetHead(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, headHash, "HEAD should point to a commit hash")
	assert.Equal(t, int64(1), ver, "Version should be 1")

	// B. 验证 S3/Disk 是否有 Commit 对象
	// 尝试从 Store 读取 HEAD 指向的对象
	reader, err := app.Store.Get(ctx, headHash)
	assert.NoError(t, err, "Commit object must exist in object storage")
	if reader != nil {
		reader.Close()
	}

	// C. 验证 Postgres 是否有索引记录 (这正是之前漏掉的！)
	var commitModel *meta.CommitModel
	commitModel, err = app.Repository.GetCommit(ctx, headHash)
	assert.NoError(t, err, "Commit metadata must exist in SQL database")
	assert.Equal(t, "Integration Test Commit", commitModel.Message, "Commit message should match")

	t.Logf("✅ Integration Test Passed: Commit %s is fully persisted (Disk + SQL + Refs)", headHash)
}
