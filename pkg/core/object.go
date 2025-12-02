package core

import "tensorvault/pkg/types"

// ObjectType 定义了 TensorVault 中的对象类型
type ObjectType string

const (
	TypeChunk    ObjectType = "chunk"    // 原始数据块 (L1)
	TypeFileNode ObjectType = "filenode" // 大文件索引 (L2, ADL)
	TypeTree     ObjectType = "tree"     // 目录树 (L3)
	TypeCommit   ObjectType = "commit"   // 版本快照 (L4)
)

// Object 是所有 Merkle DAG 节点的通用接口
type Object interface {
	// Type 返回对象类型
	Type() ObjectType

	// ID 返回对象的哈希值 (CID)
	// 注意：在对象被密封(Seal/Serialize)之前，这可能为空
	ID() types.Hash

	// Bytes 返回对象的序列化数据 (用于存储)
	Bytes() []byte
}
