package ingester

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"

	"tensorvault/pkg/core"
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

func TestIngest_Concurrency_LargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	// 1. Setup
	tmpDir := t.TempDir()
	store, err := disk.NewAdapter(tmpDir)
	require.NoError(t, err)

	ing := NewIngester(store)
	ctx := context.Background()

	// 2. 生成 5MB 随机数据
	// 5MB > 1MB ReadBuffer: 强制 Generator 循环读取，测试 Remainder 拼接逻辑
	// Random Data: 避免重复数据，确保 CDC 切分点随机分布，测试 Collector 重组逻辑
	size := 5 * 1024 * 1024
	data := make([]byte, size)
	_, err = rand.Read(data)
	require.NoError(t, err)

	// 3. Ingest
	t.Log("Starting concurrent ingest...")
	fileNode, err := ing.IngestFile(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	// 4. Verification

	// 4.1 Metadata Check
	assert.Equal(t, int64(size), fileNode.TotalSize)
	// Avg Chunk is 8KB. 5MB / 8KB ~= 640 chunks.
	// 只要大于 100 个块，就足以证明并发 Worker 池被充分利用了
	assert.Greater(t, len(fileNode.Chunks), 100, "Should result in many chunks")

	// 4.2 Reassembly & Integrity Check (核心验证)
	// 我们不依赖 Exporter，而是手动从 Store 读取所有块，拼起来跟原始数据比对
	// 这证明了：
	// 1. 数据没有丢失 (Remainder 逻辑正确)
	// 2. 顺序没有乱 (Collector 逻辑正确)
	// 3. 内容没有损坏 (Hash/Store 逻辑正确)

	var reassembled bytes.Buffer
	for i, link := range fileNode.Chunks {
		// 基本检查
		assert.Greater(t, link.Size, 0)

		// 从存储读取
		rc, err := store.Get(ctx, link.Cid.Hash)
		require.NoError(t, err, "Chunk %d missing in store", i)

		chunkBytes, err := io.ReadAll(rc)
		rc.Close()
		require.NoError(t, err)

		// 验证当前块的 Hash 是否真的匹配 (防御性检查)
		actualHash := core.CalculateBlobHash(chunkBytes)
		assert.Equal(t, link.Cid.Hash, actualHash, "Chunk %d content mismatch with hash", i)

		reassembled.Write(chunkBytes)
	}

	// 4.3 终极字节比对
	if !bytes.Equal(data, reassembled.Bytes()) {
		t.Fatal("❌ Data Corruption Detected: Reassembled data does not match original!")
	} else {
		t.Log("✅ Integrity Verified: 5MB data reassembled perfectly.")
	}
}
