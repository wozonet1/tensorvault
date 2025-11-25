package disk

import (
	"context"
	"io"
	"os"
	"testing"

	"tensorvault/pkg/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 模拟一个简单的 Object 实现，用于测试
type mockObject struct {
	id   string
	data []byte
}

func (m mockObject) ID() string            { return m.id }
func (m mockObject) Bytes() []byte         { return m.data }
func (m mockObject) Type() core.ObjectType { return core.TypeChunk }
func (m mockObject) Size() int64           { return int64(len(m.data)) }

func TestDiskAdapter(t *testing.T) {
	// 1. 创建临时测试目录
	tmpDir := t.TempDir()
	store, err := NewAdapter(tmpDir)
	require.NoError(t, err)

	ctx := context.Background()

	// 假设这是 hash("hello") 的值
	// hash: 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	obj := mockObject{
		id:   "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		data: []byte("hello world"),
	}

	// 2. 测试 Put
	err = store.Put(ctx, obj)
	assert.NoError(t, err)

	// 验证文件是否真的存在于物理磁盘
	// 路径应该是 tmpDir/2c/f24dba...
	expectedPath := tmpDir + "/2c/f24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	_, err = os.Stat(expectedPath)
	assert.NoError(t, err, "文件应该存在于 Sharding 目录中")

	// 3. 测试 Has
	exists, err := store.Has(ctx, obj.id)
	assert.NoError(t, err)
	assert.True(t, exists)

	exists, _ = store.Has(ctx, "ffffffff") // 不存在的
	assert.False(t, exists)

	// 4. 测试 Get
	reader, err := store.Get(ctx, obj.id)
	assert.NoError(t, err)
	defer reader.Close()

	content, _ := io.ReadAll(reader)
	assert.Equal(t, []byte("hello world"), content)
}
