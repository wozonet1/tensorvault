package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tensorvault/pkg/index"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/storage/disk"
	"tensorvault/pkg/storage/s3"

	"github.com/spf13/viper"
)

// App 是整个应用程序的依赖容器 (Dependency Container)
type App struct {
	Store    storage.Store
	Index    *index.Index
	Refs     *refs.Manager
	RepoPath string // 本地仓库根目录 (.tv)
}

// NewApp 是工厂函数，负责组装系统
func NewApp() (*App, error) {
	// 初始化上下文，用于 S3 连接检测等 (设置 5秒 超时防止卡死)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. 确定本地仓库路径 (.tv)
	// 逻辑：无论数据存哪，本地必须有 .tv 用来存 index 和 HEAD
	// 默认在当前目录下，或者通过配置指定
	workDir, _ := os.Getwd()
	localRepoPath := filepath.Join(workDir, ".tv")

	// 检查本地仓库是否初始化
	if _, err := os.Stat(localRepoPath); os.IsNotExist(err) {
		// 这里返回特定错误，提示用户运行 init
		// 注意：这是一个“软错误”，但在 RunE 逻辑里会被捕获
		return nil, fmt.Errorf("repository not found at %s (run 'tv init' first)", localRepoPath)
	}

	// 2. 初始化存储后端 (Storage Backend)
	store, err := initStore(ctx, localRepoPath)
	if err != nil {
		return nil, err
	}

	// 3. 初始化本地状态组件 (Index & Refs)
	indexPath := filepath.Join(localRepoPath, "index.json")
	idx, err := index.NewIndex(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load index: %w", err)
	}

	refMgr := refs.NewManager(localRepoPath)

	return &App{
		Store:    store,
		Index:    idx,
		Refs:     refMgr,
		RepoPath: localRepoPath,
	}, nil
}

// initStore 根据配置决定实例化哪种存储适配器
func initStore(ctx context.Context, localRepoPath string) (storage.Store, error) {
	storageType := viper.GetString("storage.type")

	// 默认为 disk
	if storageType == "" {
		storageType = "disk"
	}

	fmt.Printf("🔌 Initializing Storage: %s\n", strings.ToUpper(storageType))

	switch storageType {
	case "disk":
		// 磁盘模式：数据存在 .tv/objects
		storePath := viper.GetString("storage.path")
		if storePath == "" {
			// 默认路径
			storePath = filepath.Join(localRepoPath, "objects")
		}
		return disk.NewAdapter(storePath)

	case "s3":
		// S3 模式：数据存在云端
		cfg := s3.Config{
			Endpoint:        viper.GetString("storage.s3.endpoint"),
			Region:          viper.GetString("storage.s3.region"),
			Bucket:          viper.GetString("storage.s3.bucket"),
			AccessKeyID:     viper.GetString("storage.s3.access_key_id"),
			SecretAccessKey: viper.GetString("storage.s3.secret_access_key"),
		}

		// 简单的配置校验
		if cfg.Bucket == "" {
			return nil, fmt.Errorf("storage.s3.bucket is required")
		}

		return s3.NewAdapter(ctx, cfg)

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}
