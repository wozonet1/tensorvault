package ingester

import (
	"bytes"
	"context"
	"testing"

	"tensorvault/pkg/storage/disk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIngestFlow(t *testing.T) {
	// 1. 准备环境
	tmpDir := t.TempDir()
	store, err := disk.NewAdapter(tmpDir)
	require.NoError(t, err)

	ing := NewIngester(store)
	ctx := context.Background()

	// 2. 准备一个“大”文件 (100KB 随机数据，足以触发多次切分)
	// 这里简单用重复数据模拟
	content := bytes.Repeat([]byte("Hello TensorVault AI Infra "), 5000)
	// 长度大约 135KB，预期会切成 ~20 个块 (按 8KB 平均)

	// 3. 执行 Ingest
	fileNode, err := ing.IngestFile(ctx, bytes.NewReader(content))
	require.NoError(t, err)

	// 4. 验证 FileNode
	assert.Equal(t, int64(len(content)), fileNode.TotalSize)
	assert.Greater(t, len(fileNode.Chunks), 1, "应该被切分成多个块")

	t.Logf("File stored! Root Hash: %s", fileNode.ID())
	t.Logf("Total Chunks: %d", len(fileNode.Chunks))

	// 5. (可选) 验证数据是否落地
	// 检查 store 里有没有 fileNode.ID()
	exists, err := store.Has(ctx, fileNode.ID())
	require.NoError(t, err)
	assert.True(t, exists, "FileNode 应该被持久化")

	// 检查第一个 Chunk 是否存在
	firstChunkHash := fileNode.Chunks[0].Cid.Hash
	exists, err = store.Has(ctx, firstChunkHash)
	require.NoError(t, err)
	assert.True(t, exists, "Chunk 应该被持久化")
}
