package s3

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// 1. 测试辅助工具 (Mock Object)
// -----------------------------------------------------------------------------

// 简单的 Mock 对象，用于充当 Chunk
type mockObject struct {
	id   types.Hash
	data []byte
}

func (m mockObject) ID() types.Hash        { return m.id }
func (m mockObject) Bytes() []byte         { return m.data }
func (m mockObject) Type() core.ObjectType { return core.TypeChunk }
func (m mockObject) Size() int64           { return int64(len(m.data)) }

// 检查本地 MinIO 端口是否开放 (9000)
// 如果没开，跳过测试，避免报错干扰
func isMinIOAvailable(t *testing.T, _ string) bool {
	// 去掉 http:// 前缀
	host := "localhost:9000"
	conn, err := net.DialTimeout("tcp", host, 1*time.Second)
	if err != nil {
		t.Logf("⚠️ MinIO not reachable at %s. Skipping integration tests.", host)
		return false
	}
	conn.Close()
	return true
}

// -----------------------------------------------------------------------------
// 2. 集成测试 (The Real Deal)
// -----------------------------------------------------------------------------

func TestS3Adapter_Integration(t *testing.T) {
	// A. 环境检查
	testEndpoint := "http://localhost:9000"
	if !isMinIOAvailable(t, testEndpoint) {
		t.Skip("Skipping S3 integration tests (MinIO down)")
	}

	// B. 初始化 Adapter
	// 使用 docker-compose.yaml 里的默认配置
	cfg := Config{
		Endpoint:        testEndpoint,
		Region:          "us-east-1",
		Bucket:          "tensorvault-test-bucket", // 专用测试桶
		AccessKeyID:     "admin",
		SecretAccessKey: "password",
	}

	ctx := context.Background()
	store, err := NewAdapter(ctx, cfg)
	require.NoError(t, err, "Failed to connect to MinIO")

	// C. 准备测试数据
	// Hash: 8888aaaa...
	obj := mockObject{
		id:   "8888aaaa00000000000000000000000000000000000000000000000000000000",
		data: []byte("Hello S3 World from TensorVault"),
	}

	// --- 测试 1: Put ---
	t.Run("Put", func(t *testing.T) {
		err := store.Put(ctx, obj)
		assert.NoError(t, err)
	})

	// --- 测试 2: Has ---
	t.Run("Has", func(t *testing.T) {
		exists, err := store.Has(ctx, obj.id)
		assert.NoError(t, err)
		assert.True(t, exists, "Object should exist in S3")

		exists, _ = store.Has(ctx, "ffffffff00000000000000000000000000000000000000000000000000000000")
		assert.False(t, exists, "Non-existent object should return false")
	})

	// --- 测试 3: Get ---
	t.Run("Get", func(t *testing.T) {
		reader, err := store.Get(ctx, obj.id)
		assert.NoError(t, err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, obj.data, content, "Content read from S3 should match")
	})

	// --- 测试 4: ExpandHash (Sharding 逻辑验证) ---
	t.Run("ExpandHash", func(t *testing.T) {
		// 准备: 再上传一个相似前缀的对象，制造歧义
		// Hash: 8888bbbb... (前4位相同)
		obj2 := mockObject{
			id:   "8888bbbb00000000000000000000000000000000000000000000000000000000",
			data: []byte("Another object"),
		}
		require.NoError(t, store.Put(ctx, obj2))

		// Case A: 精确查找 (Unique)
		// 查找 8888aa (应该只匹配 obj)
		res, err := store.ExpandHash(ctx, "8888aa")
		assert.NoError(t, err)
		assert.Equal(t, obj.id, res)

		// Case B: 歧义查找 (Ambiguous)
		// 查找 8888 (应该匹配 obj 和 obj2)
		_, err = store.ExpandHash(ctx, "8888")
		assert.ErrorIs(t, err, storage.ErrAmbiguousHash)

		// Case C: 找不到 (Not Found)
		_, err = store.ExpandHash(ctx, "9999")
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})
}
