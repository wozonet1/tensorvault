package chunker

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChunker_Deterministic(t *testing.T) {
	// 1. 准备数据：100KB 随机数据
	// 预期 AvgSize=8KB，大概切成 12-13 块
	data := make([]byte, 100*1024)
	_, err := rand.Read(data)
	assert.NoError(t, err, "Failed to generate random data")

	c := NewChunker()

	// 2. 第一次切分
	cuts1 := c.Cut(data)
	// 3. 第二次切分 (验证确定性)
	cuts2 := c.Cut(data)
	assert.Equal(t, cuts1, cuts2, "对于相同数据，切分点必须完全一致")
}

func TestChunker_MinMaxConstraints(t *testing.T) {
	// 构造一个全 0 数据，容易触发 worst-case (全0数据的 Gear Hash 可能有规律)
	data := make([]byte, 200*1024)
	c := NewChunker()
	cuts := c.Cut(data)

	start := 0
	for i, end := range cuts {
		size := end - start

		// 验证 MinSize
		// 注意：最后一块可能小于 MinSize，这是允许的
		if i < len(cuts)-1 {
			assert.GreaterOrEqual(t, size, MinSize, "Chunk %d size %d too small", i, size)
		}

		// 验证 MaxSize
		assert.LessOrEqual(t, size, MaxSize, "Chunk %d size %d too large", i, size)

		start = end
	}
}
func TestChunker_Integration_Property(t *testing.T) {
	c := NewChunker()

	// Case 1: 随机大文件
	data1 := make([]byte, MaxSize)
	_, err := rand.Read(data1)
	require.NoError(t, err)

	cuts := c.Cut(data1) // 只返回 cuts

	// 计算最后一个 Chunk 结束的位置
	lastCutOffset := 0
	if len(cuts) > 0 {
		lastCutOffset = cuts[len(cuts)-1]
	}

	// 逻辑推导：是否全部消费？
	allConsumed := (lastCutOffset == len(data1))

	if allConsumed {
		assert.Equal(t, len(data1), lastCutOffset)
	} else {
		remainder := len(data1) - lastCutOffset
		assert.LessOrEqual(t, remainder, MinSize)
		assert.Greater(t, remainder, 0)
	}

	// Case 2: 极小数据
	data2 := make([]byte, MinSize-1)
	_, err = rand.Read(data2)
	require.NoError(t, err)
	cuts2 := c.Cut(data2)

	// 必然为空
	assert.Empty(t, cuts2, "Should have NO valid cuts for small data")
}
