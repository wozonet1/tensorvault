package core

// ChunkLink 描述了 FileNode 对底层 Chunk 的引用
type ChunkLink struct {
	Hash Link `cbor:"h"` // CHANGE: string -> Link
	Size int  `cbor:"s"` // 这个 Chunk 的大小 (关键：用于计算 offset)
}

// NewChunkLink 从一个物理 Chunk 对象生成引用链接
func NewChunkLink(c *Chunk) ChunkLink {
	return ChunkLink{
		// 自动封装 ID 为 Link 类型
		Hash: NewLink(c.ID()),

		// 自动提取大小 (注意类型转换，如果 ChunkLink.Size 是 int)
		Size: int(c.Size()),
	}
}

// FileNode (ADL) 将散乱的 Chunk 组装成一个逻辑上的大文件
type FileNode struct {
	// 自身标识
	hash     string `cbor:"-"` // 不参与序列化
	rawBytes []byte `cbor:"-"` // 缓存序列化后的数据

	// 核心数据
	TypeVal   ObjectType  `cbor:"t"`  // 必须是 "filenode"
	TotalSize int64       `cbor:"ts"` // 文件总大小
	Chunks    []ChunkLink `cbor:"cs"` // 所有的切片引用
}

// NewFileNode 创建一个新的文件索引节点
func NewFileNode(totalSize int64, chunks []ChunkLink) (*FileNode, error) {
	node := &FileNode{
		TypeVal:   TypeFileNode,
		TotalSize: totalSize,
		Chunks:    chunks,
	}
	h, b, err := CalculateHash(node)
	if err != nil {
		return nil, err
	}
	node.hash = h
	node.rawBytes = b
	return node, nil
}

func (f *FileNode) Type() ObjectType { return TypeFileNode }
func (f *FileNode) ID() string       { return f.hash }
func (f *FileNode) Bytes() []byte    { return f.rawBytes }
func (f *FileNode) Size() int64      { return f.TotalSize }

// pkg/core/builder.go

// FileNodeBuilder 封装了从 Chunk 组装 FileNode 的逻辑
// 它是 ADL (Advanced Data Layout) 的具体实现者
type FileNodeBuilder struct {
	totalSize int64
	chunks    []ChunkLink
}

func NewFileNodeBuilder() *FileNodeBuilder {
	return &FileNodeBuilder{
		chunks: make([]ChunkLink, 0, 100), // 预分配一点容量
	}
}

// Add 添加一个 Chunk 到构建序列中
func (b *FileNodeBuilder) Add(c *Chunk) {
	link := NewChunkLink(c)
	b.chunks = append(b.chunks, link)
	b.totalSize += int64(link.Size)
}

// Build 完成构建，生成不可变的 FileNode
func (b *FileNodeBuilder) Build() (*FileNode, error) {
	// Phase 3 伏笔：
	// 在这里，如果 len(b.chunks) > 10000，
	// 我们可以自动把它拆分成中间节点 (Intermediate Nodes)，构建成树。
	// 但对调用者来说，这一切都是透明的。

	return NewFileNode(b.totalSize, b.chunks)
}
