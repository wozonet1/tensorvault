package disk

import (
	"context"
	"io"
	"os"
	"testing"

	"tensorvault/pkg/core"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 模拟一个简单的 Object 实现，用于测试
type mockObject struct {
	id   types.Hash
	data []byte
}

func (m mockObject) ID() types.Hash        { return m.id }
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

	exists, err = store.Has(ctx, "ffffffff") // 不存在的
	assert.NoError(t, err)
	assert.False(t, exists)

	// 4. 测试 Get
	reader, err := store.Get(ctx, obj.id)
	assert.NoError(t, err)
	defer reader.Close()

	content, err := io.ReadAll(reader)
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello world"), content)
}

func TestDiskAdapter_ExpandHash(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewAdapter(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()

	// 准备数据: 构造两个 Hash 前缀相似的对象
	// Hash A: 1111aaaa...
	objA := mockObject{id: "1111aaaa00000000000000000000000000000000000000000000000000000000", data: []byte("A")}
	// Hash B: 1111bbbb...
	objB := mockObject{id: "1111bbbb00000000000000000000000000000000000000000000000000000000", data: []byte("B")}
	// Hash C: 2222cccc...
	objC := mockObject{id: "2222cccc00000000000000000000000000000000000000000000000000000000", data: []byte("C")}

	require.NoError(t, store.Put(ctx, objA))
	require.NoError(t, store.Put(ctx, objB))
	require.NoError(t, store.Put(ctx, objC))

	tests := []struct {
		name      string
		input     string
		wantHash  types.Hash
		wantErr   bool
		errString string // 可选，用于匹配部分错误信息
	}{
		{"Exact match", string(objC.id), objC.id, false, ""},
		{"Unique prefix (4 chars)", "2222", objC.id, false, ""},
		{"Unique prefix (long)", "2222cccc", objC.id, false, ""},
		{"Ambiguous prefix", "1111", "", true, "ambiguous"}, // 1111 同时匹配 A 和 B
		{"Not found", "ffff", "", true, "not found"},
		{"Too short", "123", "", true, "too short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.ExpandHash(ctx, types.HashPrefix(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errString != "" {
					assert.Contains(t, err.Error(), tt.errString)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantHash, got)
			}
		})
	}
}
