package cache

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// 1. SpyStore (间谍存储)
// 用于统计底层方法被调用的次数，验证请求是否穿透了缓存
// -----------------------------------------------------------------------------
type SpyStore struct {
	hasCount int32
	putCount int32
	objects  map[types.Hash][]byte
}

func NewSpyStore() *SpyStore {
	return &SpyStore{
		objects: make(map[types.Hash][]byte),
	}
}

func (s *SpyStore) Has(ctx context.Context, hash types.Hash) (bool, error) {
	atomic.AddInt32(&s.hasCount, 1) // 记录调用次数
	_, ok := s.objects[hash]
	return ok, nil
}

func (s *SpyStore) Put(ctx context.Context, obj core.Object) error {
	atomic.AddInt32(&s.putCount, 1) // 记录调用次数
	s.objects[obj.ID()] = obj.Bytes()
	return nil
}

// 其他接口存根 (Stub)
func (s *SpyStore) Get(ctx context.Context, hash types.Hash) (io.ReadCloser, error) { return nil, nil }
func (s *SpyStore) ExpandHash(ctx context.Context, short types.HashPrefix) (types.Hash, error) {
	return "", nil
}

// -----------------------------------------------------------------------------
// 2. Mock Object
// -----------------------------------------------------------------------------
type mockObject struct {
	id types.Hash
}

func (m mockObject) ID() types.Hash        { return m.id }
func (m mockObject) Bytes() []byte         { return []byte("fake data") }
func (m mockObject) Type() core.ObjectType { return core.TypeChunk }

// -----------------------------------------------------------------------------
// 3. 集成测试
// -----------------------------------------------------------------------------

func TestCachedStore_Integration(t *testing.T) {
	// A. 环境检查: 确保 Redis 在运行
	redisAddr := "localhost:6379"
	conn, err := net.DialTimeout("tcp", redisAddr, 1*time.Second)
	if err != nil {
		t.Skipf("Skipping Redis integration test: %v", err)
	}
	conn.Close()

	// B. 初始化
	ctx := context.Background()
	spy := NewSpyStore()
	redisURL := fmt.Sprintf("redis://%s/0", redisAddr)
	cfg := Config{
		RedisURL: redisURL,
		TTL:      1 * time.Hour,
	}
	cachedStore, err := NewCachedStore(spy, cfg)
	require.NoError(t, err)

	// 清理 Redis (防止上次测试残留)
	cachedStore.client.FlushDB(ctx)

	// 准备测试数据
	hash := types.Hash("1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff")
	obj := mockObject{id: hash}

	// --- Step 1: Cache Miss ---
	t.Log("Step 1: Check non-existent object (Cache Miss)")
	exists, err := cachedStore.Has(ctx, hash)
	require.NoError(t, err)
	assert.False(t, exists)

	// 验证：底层 Spy 的 Has 应该被调用了 1 次
	assert.Equal(t, int32(1), atomic.LoadInt32(&spy.hasCount), "Backend Has() should be called on miss")

	// --- Step 2: Put (Write-Through) ---
	t.Log("Step 2: Put object (Update Cache)")
	err = cachedStore.Put(ctx, obj)
	require.NoError(t, err)

	// 验证：底层 Spy 的 Put 应该被调用了 1 次
	assert.Equal(t, int32(1), atomic.LoadInt32(&spy.putCount), "Backend Put() should be called")

	// 验证：Redis 应该有这个 Key 了
	// 我们直接查 Redis 确认
	key := cachedStore.cacheKey(hash)
	redisVal, err := cachedStore.client.Exists(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), redisVal, "Redis key should be set after Put")

	// --- Step 3: Cache Hit (The Moment of Truth) ---
	t.Log("Step 3: Check existing object again (Cache Hit)")
	exists, err = cachedStore.Has(ctx, hash)
	require.NoError(t, err)
	assert.True(t, exists)

	// 核心断言：Spy 的 Has 调用次数应该 *依然是 2*
	// 这证明了请求被 Redis 拦截，根本没到底层
	assert.Equal(t, int32(2), atomic.LoadInt32(&spy.hasCount), "Backend Has() should NOT be called on hit")

	if atomic.LoadInt32(&spy.hasCount) == 2 {
		t.Log("✅ SUCCESS: Traffic intercepted by Redis!")
	} else {
		t.Fatal("❌ FAILURE: Leaky abstraction, traffic hit the backend.")
	}
}
