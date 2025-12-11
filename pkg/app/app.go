package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tensorvault/pkg/exporter"
	"tensorvault/pkg/index"
	"tensorvault/pkg/meta"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/storage/cache"
	"tensorvault/pkg/storage/disk"
	"tensorvault/pkg/storage/s3"

	"github.com/spf13/viper"
)

// App 是整个应用程序的依赖容器 (Dependency Container)
type App struct {
	Store      storage.Store
	Index      *index.Index
	Refs       *refs.Manager
	RepoPath   string // 本地仓库根目录 (.tv)
	Repository *meta.Repository
}

// NewApp 是工厂函数，负责组装系统
func NewApp() (*App, error) {
	var metaDB *meta.DB
	var repository *meta.Repository
	var refMgr *refs.Manager
	// 初始化上下文，用于 S3 连接检测等 (设置 5秒 超时防止卡死)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. 确定本地仓库路径 (.tv)
	// 逻辑：无论数据存哪，本地必须有 .tv 用来存 index 和 HEAD
	// 默认在当前目录下，或者通过配置指定
	workDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	localRepoPath := filepath.Join(workDir, ".tv")

	// 检查本地仓库是否初始化
	if _, err := os.Stat(localRepoPath); os.IsNotExist(err) {
		// 这里返回特定错误，提示用户运行 init
		// 注意：这是一个“软错误”，但在 RunE 逻辑里会被捕获
		return nil, fmt.Errorf("repository not found at %s (run 'tv init' first)", localRepoPath)
	}
	dbCfg := meta.Config{
		Host:     viper.GetString("database.host"),
		Port:     viper.GetInt("database.port"),
		User:     viper.GetString("database.user"),
		Password: viper.GetString("database.password"),
		DBName:   viper.GetString("database.dbname"),
		SSLMode:  viper.GetString("database.sslmode"),
	}

	conn, err := meta.NewDB(ctx, dbCfg)
	if err != nil {
		// [关键] 打印黄色的警告，而不是红色的错误
		// 这里的判断逻辑可以更细致：如果配置明显是空的，甚至连警告都不打
		if dbCfg.User != "" {
			fmt.Printf("⚠️  Warning: Metadata DB not available (%v). Local commit/branching will be disabled.\n", err)
		}
	} else {
		metaDB = conn
		repository = meta.NewRepository(metaDB)
		refMgr = refs.NewManager(repository)
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

	return &App{
		Store:      store,
		Index:      idx,
		Refs:       refMgr,
		RepoPath:   localRepoPath,
		Repository: repository,
	}, nil
}

// -----------------------------------------------------------------------------
// Helper Methods (服务定位器模式)
// -----------------------------------------------------------------------------

// GetExporter 创建并返回一个新的 Exporter 实例
// 为什么不把它作为 App 的字段？因为 Exporter 通常是无状态的，
// 而且每次操作可能涉及不同的 Context 或配置微调，On-demand 创建更灵活且开销极低。
func (a *App) GetExporter() *exporter.Exporter {
	return exporter.NewExporter(a.Store)
}

// initStore 根据配置组装存储层 (Base Store + Cache Layer)
func initStore(ctx context.Context, localRepoPath string) (storage.Store, error) {
	var baseStore storage.Store
	var err error

	// 1. 初始化底层物理存储 (Base Store)
	storageType := viper.GetString("storage.type")
	if storageType == "" {
		storageType = "disk"
	}

	fmt.Printf("🔌 Storage Backend: %s\n", strings.ToUpper(storageType))

	switch storageType {
	case "disk":
		storePath := viper.GetString("storage.path")
		if storePath == "" {
			storePath = filepath.Join(localRepoPath, "objects")
		}
		baseStore, err = disk.NewAdapter(storePath)

	case "s3":
		cfg := s3.Config{
			Endpoint:        viper.GetString("storage.s3.endpoint"),
			Region:          viper.GetString("storage.s3.region"),
			Bucket:          viper.GetString("storage.s3.bucket"),
			AccessKeyID:     viper.GetString("storage.s3.access_key_id"),
			SecretAccessKey: viper.GetString("storage.s3.secret_access_key"),
		}
		if cfg.Bucket == "" {
			return nil, fmt.Errorf("storage.s3.bucket is required")
		}
		baseStore, err = s3.NewAdapter(ctx, cfg)

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}

	if err != nil {
		return nil, err
	}

	// 2. 初始化缓存层 (Cache Layer Decorator)
	// 检查配置是否启用了缓存
	// TODO:配置使用Config结构体读取更清晰，但为了简单起见这里直接用 viper
	if viper.GetBool("storage.cache.enabled") {
		redisURL := viper.GetString("storage.cache.redis_url")
		if redisURL == "" {
			redisURL = "redis://localhost:6379/0"
		}

		ttl := viper.GetDuration("storage.cache.ttl")
		if ttl == 0 {
			ttl = 24 * time.Hour
		}

		fmt.Printf("🚀 Cache Layer: Enabled (Redis @ %s)\n", redactPassword(redisURL))

		// Change: 使用 Config 结构体初始化
		cacheCfg := cache.Config{
			RedisURL: redisURL,
			TTL:      ttl,
		}
		// 【关键】用 CachedStore 包裹 baseStore
		// 此时返回的 store 对象，其 Has/Put 方法都会先经过 Redis
		baseStore, err = cache.NewCachedStore(baseStore, cacheCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to init redis cache: %w", err)
		}
	} else {
		fmt.Println("🐌 Cache Layer: Disabled")
	}

	return baseStore, nil
}

// 辅助函数：隐藏 Redis URL 中的密码，避免日志泄露
func redactPassword(url string) string {
	// 简单实现，生产环境可以用 url.Parse 处理
	// redis://user:password@host... -> redis://user:****@host...
	return url
}
