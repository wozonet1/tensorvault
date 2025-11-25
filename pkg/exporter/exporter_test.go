package exporter

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tensorvault/pkg/ingester"
	"tensorvault/pkg/storage/disk"
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
