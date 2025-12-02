package chunker

import (
	"math"
)

// 针对 AI 大文件场景的配置 (单位: 字节)
const (
	MinSize   = 4 * 1024  // 4KB
	AvgSize   = 8 * 1024  // 8KB (生产环境建议设为 2MB-4MB，测试环境用小一点方便观察)
	MaxSize   = 64 * 1024 // 64KB
	NormLevel = 2
)

// Chunker 是一个无状态的切分工具
type Chunker struct {
	maskS uint64
	maskL uint64
}

func NewChunker() *Chunker {
	// 预计算掩码 (和实验代码一致)
	bits := int(math.Round(math.Log2(float64(AvgSize))))
	return &Chunker{
		maskS: uint64(1<<(bits+NormLevel)) - 1,
		maskL: uint64(1<<(bits-NormLevel)) - 1,
	}
}

// Cut 将数据切分成一系列的切点。
// 返回值:
//   []int: 所有的 **完整块** 的结束 offset。不包含未处理完的尾部。

func (c *Chunker) Cut(data []byte) []int {
	var cutPoints []int
	offset := 0
	n := len(data)

	for offset < n {
		// 1. 剩余不足最小块，直接收尾
		if n-offset <= MinSize {
			return cutPoints
		}

		// 2. 初始化状态
		// 每次新块开始，fp 重置为 0
		fp := uint64(0)
		idx := offset + MinSize

		// 确定边界
		normLimit := min(offset+AvgSize, n)
		maxLimit := min(offset+MaxSize, n)

		// 定义扫描闭包 (DRY)
		scan := func(limit int, mask uint64) bool {
			for ; idx < limit; idx++ {
				fp = (fp << 1) + gearTable[data[idx]]
				// 判断掩码
				if (fp & mask) == 0 {
					cutPoints = append(cutPoints, idx+1)
					offset = idx + 1
					return true
				}
			}
			return false
		}

		// A. 归一化区域 (严掩码)
		if scan(normLimit, c.maskS) {
			continue
		}

		// B. 普通区域 (宽掩码)
		if scan(maxLimit, c.maskL) {
			continue
		}

		// C. 强制切分
		cutPoints = append(cutPoints, maxLimit)
		offset = maxLimit
	}

	return cutPoints
}
