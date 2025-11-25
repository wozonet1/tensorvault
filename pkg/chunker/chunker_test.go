package chunker

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChunker_Deterministic(t *testing.T) {
	// 1. 准备数据：100KB 随机数据
	// 预期 AvgSize=8KB，大概切成 12-13 块
	data := make([]byte, 100*1024)
	rand.Read(data)

	c := NewChunker()

	// 2. 第一次切分
	cuts1 := c.Cut(data)
	assert.NotEmpty(t, cuts1)
	assert.Equal(t, len(data), cuts1[len(cuts1)-1], "最后一块必须结束于文件末尾")

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
