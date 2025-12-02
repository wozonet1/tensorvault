package core

import "tensorvault/pkg/types"

// Chunk 代表 FastCDC 切分出来的物理数据块
// 它是 Merkle DAG 的叶子节点
type Chunk struct {
	hash types.Hash
	data []byte
}

func NewChunk(data []byte) *Chunk {
	// 计算 Hash
	h := CalculateBlobHash(data)
	return &Chunk{
		hash: h,
		data: data,
	}
}

func (c *Chunk) Type() ObjectType { return TypeChunk }
func (c *Chunk) ID() types.Hash   { return c.hash }
func (c *Chunk) Bytes() []byte    { return c.data }
func (c *Chunk) Size() int64      { return int64(len(c.data)) }
