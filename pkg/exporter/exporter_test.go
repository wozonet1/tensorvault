package exporter

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tensorvault/pkg/core"
	"tensorvault/pkg/ingester"
	"tensorvault/pkg/storage/disk"
	"tensorvault/pkg/types"
)

func TestIngestAndExport_RoundTrip(t *testing.T) {
	// 1. 准备环境 (内存或者临时磁盘)
	tmpDir := t.TempDir()
	store, err := disk.NewAdapter(tmpDir)
	require.NoError(t, err)

	ing := ingester.NewIngester(store)
	exp := NewExporter(store)
	ctx := context.Background()

	// 2. 生成随机大文件数据 (模拟 500KB，足以触发多次切分)
	// 为什么不用 10GB？因为这是单元测试，要快。逻辑是一样的。
	originalData := make([]byte, 500*1024)
	_, err = rand.Read(originalData)
	require.NoError(t, err)

	// 3. Ingest (写入)
	t.Log("Step 1: Ingesting...")
	fileNode, err := ing.IngestFile(ctx, bytes.NewReader(originalData))
	require.NoError(t, err)

	rootHash := fileNode.ID()
	t.Logf("File Ingested. Root Hash: %s", rootHash)
	t.Logf("Chunks count: %d", len(fileNode.Chunks))

	// 4. Export (读取)
	t.Log("Step 2: Exporting...")
	var restoredBuffer bytes.Buffer
	err = exp.ExportFile(ctx, rootHash, &restoredBuffer)
	require.NoError(t, err)

	// 5. 终极比对 (Bit-wise Comparison)
	t.Log("Step 3: Comparing...")
	assert.Equal(t, len(originalData), restoredBuffer.Len(), "文件大小应该一致")

	if bytes.Equal(originalData, restoredBuffer.Bytes()) {
		t.Log("✅ SUCCESS: Data Restored Perfectly! (完美还原)")
	} else {
		t.Fatal("❌ FAILURE: Data Mismatch! (数据损坏)")
	}
}

// TestRestoreAndPrint_Integration 是一个集成测试
// 它模拟了 Commit -> Tree -> FileNode -> Chunk 的完整链条
func TestRestoreAndPrint_Integration(t *testing.T) {
	// 1. Setup
	store, err := disk.NewAdapter(t.TempDir())
	require.NoError(t, err)
	exp := NewExporter(store)
	ctx := context.Background()

	// 2. 手动构造一个微型 DAG
	// Chunk
	chunkData := []byte("hello restore")
	chunk := core.NewChunk(chunkData)
	require.NoError(t, store.Put(ctx, chunk))

	// FileNode
	fileNode, err := core.NewFileNode(int64(len(chunkData)), []core.ChunkLink{core.NewChunkLink(chunk)})
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, fileNode))

	// Tree (Root -> "test.txt")
	treeEntry := core.NewFileEntry("test.txt", fileNode.ID(), fileNode.TotalSize)
	tree, err := core.NewTree([]core.TreeEntry{treeEntry})
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, tree))

	// Commit
	commit, err := core.NewCommit(tree.ID(), nil, "Tester", "Init")
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, commit))

	// ---------------------------------------------------
	// Test A: RestoreTree (Checkout Logic)
	// ---------------------------------------------------
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

	// ---------------------------------------------------
	// Test B: PrintObject (Log/Cat Logic)
	// ---------------------------------------------------
	var buf bytes.Buffer

	// Case 1: Print Commit
	buf.Reset()
	err = exp.PrintObject(ctx, commit.ID(), &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Type:    Commit")
	assert.Contains(t, buf.String(), "Tester")

	// Case 2: Print Tree
	buf.Reset()
	err = exp.PrintObject(ctx, tree.ID(), &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Type: Tree")
	assert.Contains(t, buf.String(), "test.txt")

	// Case 3: Print FileNode
	buf.Reset()
	err = exp.PrintObject(ctx, fileNode.ID(), &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Type:      FileNode")
}
