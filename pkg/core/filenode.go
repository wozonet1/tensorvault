package core

// ChunkLink 描述了 FileNode 对底层 Chunk 的引用
type ChunkLink struct {
	Hash Link `cbor:"h"` // CHANGE: string -> Link
	Size int  `cbor:"s"` // 这个 Chunk 的大小 (关键：用于计算 offset)
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
