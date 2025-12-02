package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/exporter"
	"tensorvault/pkg/ingester"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/storage/cache"
	"tensorvault/pkg/storage/disk"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SpyStore 再次登场，用于验证缓存命中
// 这次我们只关心它被调用的次数，不关心内部存储逻辑（因为我们用 DiskAdapter 做真正的存储）
type MetricStore struct {
	storage.Store // 组合真正的 Store
	putCount      int32
	hasCount      int32
}

func (m *MetricStore) Put(ctx context.Context, obj core.Object) error {
	atomic.AddInt32(&m.putCount, 1)
	return m.Store.Put(ctx, obj)
}

func (m *MetricStore) Has(ctx context.Context, hash types.Hash) (bool, error) {
	atomic.AddInt32(&m.hasCount, 1)
	return m.Store.Has(ctx, hash)
}

// TestPhase1_Workflow 验证 Phase 1 的所有核心特性：
// 并发切分 -> Redis 缓存写入 -> 缓存命中去重 -> 并发恢复
func TestPhase1_Workflow(t *testing.T) {
	// 1. 基础设施准备
	// -------------------------------------------------------------
	redisAddr := "localhost:6379"
	if conn, err := net.DialTimeout("tcp", redisAddr, 1*time.Second); err != nil {
		t.Skip("Skipping E2E test: Redis not available")
	} else {
		conn.Close()
	}

	tmpDir := t.TempDir()

	// 基础存储 (Disk)
	diskStore, err := disk.NewAdapter(filepath.Join(tmpDir, "objects"))
	require.NoError(t, err)

	// 监控层 (Metrics)
	spy := &MetricStore{Store: diskStore}

	// 缓存层 (Redis)
	redisURL := fmt.Sprintf("redis://%s/0", redisAddr)
	cacheCfg := cache.Config{RedisURL: redisURL, TTL: 1 * time.Hour}
	cachedStore, err := cache.NewCachedStore(spy, cacheCfg)
	require.NoError(t, err)

	// 清空 Redis 确保测试纯净
	// (注意：这里需要一点 hack 或直接用 redis client，为了简单我们假设 Redis 是干净的或 key 不冲突)

	ctx := context.Background()

	// 2. 准备数据 (20MB 随机数据)
	// -------------------------------------------------------------
	t.Log("Generating 20MB random data...")
	dataSize := 20 * 1024 * 1024
	originalData := make([]byte, dataSize)
	_, err = rand.Read(originalData)
	require.NoError(t, err)

	// 3. 第一次上传 (Cold Upload)
	// -------------------------------------------------------------
	t.Log("Step 1: Cold Ingest (Should write to Disk & Redis)...")
	ing := ingester.NewIngester(cachedStore)

	start := time.Now()
	node1, err := ing.IngestFile(ctx, bytes.NewReader(originalData))
	require.NoError(t, err)
	t.Logf("Cold Ingest took: %v", time.Since(start))

	// 验证：底层 Put 应该被调用 (Chunk数量 + 1个FileNode)
	chunksCount := len(node1.Chunks)
	assert.Greater(t, int(atomic.LoadInt32(&spy.putCount)), chunksCount, "Should write chunks to disk")

	// 记录当前的调用次数
	putsAfterCold := atomic.LoadInt32(&spy.putCount)

	// 4. 第二次上传 (Warm Upload / Dedup)
	// -------------------------------------------------------------
	t.Log("Step 2: Warm Ingest (Should hit Redis Cache)...")

	start = time.Now()
	node2, err := ing.IngestFile(ctx, bytes.NewReader(originalData))
	require.NoError(t, err)
	t.Logf("Warm Ingest took: %v", time.Since(start))

	// 验证一致性
	assert.Equal(t, node1.ID(), node2.ID(), "Hash should match")

	// 验证去重：底层 Put 次数应该几乎不变
	// (注意：Ingester 可能会 Put 重复的 FileNode，这取决于 Ingester 逻辑，但 Chunks 肯定不会重新 Put)
	// 即使 FileNode 重新 Put 了，Chunk 的 Put 增量应该是 0。
	putsAfterWarm := atomic.LoadInt32(&spy.putCount)

	// 我们允许最多增加 1 次 (FileNode 本身)，但绝不能增加 chunksCount 次
	diff := putsAfterWarm - putsAfterCold
	assert.LessOrEqual(t, int(diff), 1, "Warm ingest should trigger ZERO chunk uploads due to cache")

	if int(diff) <= 1 {
		t.Log("✅ Deduplication works! No chunks re-uploaded.")
	}

	// 5. 并发恢复 (Concurrent Restore)
	// -------------------------------------------------------------
	t.Log("Step 3: Concurrent Restore...")
	exp := exporter.NewExporter(cachedStore)
	restorePath := filepath.Join(tmpDir, "restored.bin")

	// 创建文件以触发 WriterAt 并发逻辑
	f, err := os.Create(restorePath)
	require.NoError(t, err)

	start = time.Now()
	err = exp.ExportFile(ctx, node1.ID(), f)
	f.Close()
	require.NoError(t, err)
	t.Logf("Restore took: %v", time.Since(start))

	// 6. 数据完整性比对
	// -------------------------------------------------------------
	restoredData, err := os.ReadFile(restorePath)
	require.NoError(t, err)

	if bytes.Equal(originalData, restoredData) {
		t.Log("✅ SUCCESS: Phase 1 E2E Test Passed!")
	} else {
		t.Fatal("❌ FAILURE: Data mismatch")
	}
}
