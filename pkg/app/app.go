// pkg/app/app.go
package app

import (
	"fmt"
	"path/filepath"

	"tensorvault/pkg/index"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/storage/disk"

	"github.com/spf13/viper"
)

// App 是整个应用程序的依赖容器 (Dependency Container)
// 它持有所有“单例”服务
type App struct {
	Store storage.Store
	Index *index.Index
	Refs  *refs.Manager
	// 如果未来有 Logger, Config 对象，也放这里
	RepoPath string
}

// NewApp 是工厂函数，负责组装这一台机器
// 它遵循 Viper 的配置，但不知道具体的 CLI 命令
func NewApp() (*App, error) {
	// 1. 获取仓库根路径 (Single Source of Truth)
	storePath := viper.GetString("storage.path")
	if storePath == "" {
		return nil, fmt.Errorf("storage path not set")
	}

	// 2. 初始化存储层 (Dependency Injection)
	// 如果未来要支持 S3，就在这里通过 switch case 切换
	// store := s3.NewAdapter(...)
	store, err := disk.NewAdapter(storePath)
	if err != nil {
		return nil, fmt.Errorf("failed to init storage: %w", err)
	}

	// 3. 初始化暂存区
	// 假设 index 文件位于 objects 同级的上一层，即 .tv/index
	// storePath: .../.tv/objects
	// repoPath:  .../.tv
	repoPath := filepath.Dir(storePath)
	indexPath := filepath.Join(repoPath, "index.json")

	idx, err := index.NewIndex(indexPath)
	if err != nil {
		// 注意：如果这只是单纯的没初始化，可能不该报错？
		// 但作为 Factory，如果环境不对，报错是安全的。
		// 让 init 命令去处理初始化逻辑。
		return nil, fmt.Errorf("failed to load index: %w", err)
	}

	refMgr := refs.NewManager(repoPath)

	return &App{
		Store:    store,
		Index:    idx,
		Refs:     refMgr,
		RepoPath: repoPath,
	}, nil
}
