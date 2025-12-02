package cache

import (
	"context"
	"fmt"
	"io"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/types"

	"github.com/redis/go-redis/v9"
)

// CachedStore 是一个装饰器，它为底层的 storage.Store 添加 Redis 缓存层
type CachedStore struct {
	backend storage.Store // 被装饰的底层存储 (如 S3)
	client  *redis.Client // Redis 客户端
	ttl     time.Duration // 缓存过期时间 (例如 24h)
}
type Config struct {
	RedisURL string        // 标准连接字符串: redis://<user>:<password>@<host>:<port>/<db>
	TTL      time.Duration // 过期时间
	// 未来可扩展:
	// PoolSize int
	// DialTimeout time.Duration
}

// Change: 接收 Config 结构体，而不是散乱的参数
func NewCachedStore(backend storage.Store, cfg Config) (*CachedStore, error) {
	// 解析 URL
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}

	client := redis.NewClient(opts)

	// Fail-fast 连接检查
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &CachedStore{
		backend: backend,
		client:  client,
		ttl:     cfg.TTL,
	}, nil
}

// cacheKey 生成 Redis Key，添加前缀防止冲突
func (s *CachedStore) cacheKey(hash types.Hash) string {
	return "tv:obj:" + string(hash)
}

// Has 优先查 Redis，实现毫秒级去重
func (s *CachedStore) Has(ctx context.Context, hash types.Hash) (bool, error) {
	key := s.cacheKey(hash)

	// 1. 查 Redis
	// Exists 返回 1 表示存在，0 表示不存在
	val, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		// 架构决策：缓存故障降级 (Cache Failure Fallback)
		// 如果 Redis 挂了，我们不应该让整个程序崩溃，而是退化为无缓存模式，直接查 S3。
		// 在生产环境中，这里应该打一个 Warning Log。
		fmt.Printf("WARN: Redis error: %v\n", err)
	} else if val > 0 {
		// Cache Hit!
		// 无需发起 S3 网络请求，直接返回。这是性能提升的关键。
		return true, nil
	}

	// 2. 缓存未命中 (Cache Miss)，查底层存储
	found, err := s.backend.Has(ctx, hash)
	if err != nil {
		return false, err
	}

	// 3. 缓存回填 (Cache Fill)
	if found {
		// 关键点：异步写入 Redis，不要阻塞主流程
		// 使用 context.Background() 确保即使上层 ctx 取消，回填也能完成
		go func() {
			fillCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s.client.Set(fillCtx, key, "1", s.ttl)
		}()
	}

	return found, nil
}

// Put 上传对象。利用 Has 的缓存能力进行预检。
func (s *CachedStore) Put(ctx context.Context, obj core.Object) error {
	// 1. 利用上面的 Has 方法检查存在性
	// 如果 Redis 里有，这一步耗时 < 1ms，直接跳过上传
	exists, err := s.Has(ctx, obj.ID())
	if err != nil {
		return err
	}
	if exists {
		return nil // 幂等性：已存在
	}

	// 2. 穿透到底层存储 (上传 S3)
	if err := s.backend.Put(ctx, obj); err != nil {
		return err
	}

	// 3. 写入缓存
	// 只有 S3 上传成功了，才写 Redis
	key := s.cacheKey(obj.ID())
	// 这里的 Set 错误可以忽略，不影响主流程
	s.client.Set(ctx, key, "1", s.ttl)

	return nil
}

// Get 透传 - 我们不缓存 Blob 数据
// 原因：AI Chunk 可能很大，Redis 内存极其宝贵，只存元数据(Existence)性价比最高。
func (s *CachedStore) Get(ctx context.Context, hash types.Hash) (io.ReadCloser, error) {
	return s.backend.Get(ctx, hash)
}

// ExpandHash 透传
func (s *CachedStore) ExpandHash(ctx context.Context, short types.HashPrefix) (types.Hash, error) {
	return s.backend.ExpandHash(ctx, short)
}
