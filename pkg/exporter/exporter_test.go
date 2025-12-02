package exporter

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"tensorvault/pkg/core"
	"tensorvault/pkg/ingester"
	"tensorvault/pkg/storage/disk"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExport_SmartDispatch 验证智能分发逻辑
// 既要测试 WriterAt (并发路径)，也要测试普通 Writer (串行路径)
func TestExport_SmartDispatch(t *testing.T) {
	// 1. Setup Environment
	tmpDir := t.TempDir()
	store, err := disk.NewAdapter(tmpDir)
	require.NoError(t, err)

	ing := ingester.NewIngester(store)
	exp := NewExporter(store)
	ctx := context.Background()

	// 2. 准备数据: 1MB 随机数据 (足以切分为 ~128 个 Chunk)
	// 这样并发下载才有意义
	dataSize := 1 * 1024 * 1024
	originalData := make([]byte, dataSize)
	_, err = rand.Read(originalData)
	require.NoError(t, err)

	// 3. Ingest (写入)
	fileNode, err := ing.IngestFile(ctx, bytes.NewReader(originalData))
	require.NoError(t, err)
	rootHash := fileNode.ID()

	// --- Case A: 串行恢复 (Serial Path) ---
	// bytes.Buffer 只实现了 io.Writer，没有实现 io.WriterAt
	// 预期：Exporter 自动降级为串行流式下载
	t.Run("Serial_Writer", func(t *testing.T) {
		var buf bytes.Buffer
		err := exp.ExportFile(ctx, rootHash, &buf)
		require.NoError(t, err)

		assert.Equal(t, dataSize, buf.Len())
		assert.True(t, bytes.Equal(originalData, buf.Bytes()), "Serial restore data mismatch")
	})

	// --- Case B: 并发恢复 (Concurrent Path) ---
	// *os.File 实现了 io.WriterAt
	// 预期：Exporter 启用 Goroutine Pool 并发乱序写入
	t.Run("Concurrent_WriterAt", func(t *testing.T) {
		restorePath := filepath.Join(tmpDir, "restored.bin")
		f, err := os.Create(restorePath)
		require.NoError(t, err)

		// 核心调用
		err = exp.ExportFile(ctx, rootHash, f)
		f.Close() // 必须关闭以刷新 buffers (虽然 WriteAt 通常是直接落盘或系统缓冲)
		require.NoError(t, err)

		// 验证内容
		restoredData, err := os.ReadFile(restorePath)
		require.NoError(t, err)
		assert.Equal(t, dataSize, len(restoredData))
		assert.True(t, bytes.Equal(originalData, restoredData), "Concurrent restore data mismatch")
	})
}

// 保留原有的集成测试，用于验证 Tree 结构的恢复
func TestRestoreAndPrint_Integration(t *testing.T) {
	// 1. Setup
	store, err := disk.NewAdapter(t.TempDir())
	require.NoError(t, err)
	exp := NewExporter(store)
	ctx := context.Background()

	// 2. 手动构造一个微型 DAG
	chunkData := []byte("hello restore")
	chunk := core.NewChunk(chunkData)
	require.NoError(t, store.Put(ctx, chunk))

	fileNode, err := core.NewFileNode(int64(len(chunkData)), []core.ChunkLink{core.NewChunkLink(chunk)})
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, fileNode))

	treeEntry := core.NewFileEntry("test.txt", fileNode.ID(), fileNode.TotalSize)
	tree, err := core.NewTree([]core.TreeEntry{treeEntry})
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, tree))

	commit, err := core.NewCommit(tree.ID(), nil, "Tester", "Init")
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, commit))

	// Test RestoreTree
	restoreDir := t.TempDir()
	callbackCalled := false

	err = exp.RestoreTree(ctx, tree.ID(), restoreDir, func(path string, hash types.Hash, size int64) {
		callbackCalled = true
		assert.Equal(t, fileNode.ID(), hash)
		assert.Contains(t, path, "test.txt")
	})
	require.NoError(t, err)
	assert.True(t, callbackCalled, "Callback should be triggered")

	// 验证文件内容
	restoredContent, err := os.ReadFile(filepath.Join(restoreDir, "test.txt"))
	require.NoError(t, err)
	assert.Equal(t, chunkData, restoredContent)

	// Test PrintObject
	var buf bytes.Buffer
	err = exp.PrintObject(ctx, commit.ID(), &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Type:    Commit")
}
