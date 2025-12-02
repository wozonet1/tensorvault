package index

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndex_Persistence_RoundTrip(t *testing.T) {
	// 1. Setup
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.json")

	// 2. 创建并写入数据
	idx1, err := NewIndex(indexPath)
	require.NoError(t, err)

	idx1.Add("data/model.bin", "hash-123", 1024)
	idx1.Add("readme.md", "hash-abc", 500)

	err = idx1.Save()
	require.NoError(t, err)

	// 3. 重新加载 (模拟第二次运行程序)
	idx2, err := NewIndex(indexPath)
	require.NoError(t, err)

	// 4. 验证数据一致性
	assert.Equal(t, 2, len(idx2.Entries))

	entry, exists := idx2.Entries["data/model.bin"]
	assert.True(t, exists)
	assert.Equal(t, "hash-123", entry.Hash.String())
	assert.Equal(t, int64(1024), entry.Size)

	// 验证时间是否被正确序列化
	assert.False(t, entry.ModifiedAt.IsZero())
}

func TestIndex_Concurrency(t *testing.T) {
	// 简单的并发安全测试
	tmpDir := t.TempDir()
	idx, err := NewIndex(filepath.Join(tmpDir, "index.json"))
	require.NoError(t, err)

	// 启动 10 个 goroutine 同时写
	done := make(chan bool)
	for range 10 {
		go func() {
			idx.Add("file", "hash", 1) // 反复写同一个 key
			done <- true
		}()
	}

	for range 10 {
		<-done
	}

	// 如果没有 panic (Map并发读写错误)，就算通过
	assert.Equal(t, 1, len(idx.Entries))
}
func TestIndex_Lifecycle(t *testing.T) {
	// 1. Setup
	tmpDir := t.TempDir()
	idx, err := NewIndex(filepath.Join(tmpDir, "index.json"))
	require.NoError(t, err)

	// 2. Test Add & IsEmpty
	assert.True(t, idx.IsEmpty())
	idx.Add("src/main.go", "hash1", 100)
	assert.False(t, idx.IsEmpty())

	// 3. Test Remove
	idx.Remove("src/main.go")
	_, exists := idx.Entries["src/main.go"]
	assert.False(t, exists, "Entry should be removed")

	// 4. Test Remove Non-existent (Idempotency)
	idx.Remove("ghost.file") // Should not panic

	// 5. Test Reset
	idx.Add("a", "h1", 1)
	idx.Add("b", "h2", 2)
	idx.Reset()
	assert.True(t, idx.IsEmpty(), "Index should be empty after Reset")
	assert.Equal(t, 0, len(idx.Entries))
}

func TestCleanPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"a/b/c", "a/b/c"},
		{"./a/b", "a/b"},
		{"a//b", "a/b"},
		{"a/../b", "b"},
		{".", "."},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, CleanPath(tt.input))
	}
}
