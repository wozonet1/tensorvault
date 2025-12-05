package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"tensorvault/pkg/app"
	"tensorvault/pkg/core"
	"tensorvault/pkg/index"
	"tensorvault/pkg/meta"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/storage/disk"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestApp 是所有 Service 测试共享的基础设施初始化逻辑
// 它返回构建好的 App 实例
func setupTestApp(t *testing.T) *app.App {
	tmpDir := t.TempDir()

	// 1. Store
	storePath := filepath.Join(tmpDir, "objects")
	require.NoError(t, os.MkdirAll(storePath, 0755))
	store, err := disk.NewAdapter(storePath)
	require.NoError(t, err)

	// 2. Index
	idxPath := filepath.Join(tmpDir, "index.json")
	require.NoError(t, os.WriteFile(idxPath, []byte("{}"), 0644))
	idx, err := index.NewIndex(idxPath)
	require.NoError(t, err)

	// 3. DB & Meta
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	metaDB := meta.NewWithConn(db)
	require.NoError(t, metaDB.AutoMigrate(&meta.Ref{}, &meta.CommitModel{}, &meta.FileIndex{}))

	repo := meta.NewRepository(metaDB)
	refMgr := refs.NewManager(repo)

	return &app.App{
		Store:      store,
		Index:      idx,
		Refs:       refMgr,
		RepoPath:   tmpDir,
		Repository: repo,
	}
}

// 辅助函数：生成合法 Hash
func mockHash(input string) types.Hash {
	return core.CalculateBlobHash([]byte(input))
}
