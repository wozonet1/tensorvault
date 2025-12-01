package meta

import (
	"time"

	"gorm.io/datatypes"
)

// Ref 存储分支指针 (例如 "refs/heads/main")
// 对应 Git 的 .git/refs/*
type Ref struct {
	// Name 是主键，例如 "HEAD" 或 "refs/heads/main"
	Name string `gorm:"primaryKey;type:varchar(255)"`

	// CommitHash 指向当前的 Commit ID
	CommitHash string `gorm:"type:char(64);not null"`

	// Version 用于乐观锁并发控制 (CAS)
	// 每次更新时 +1，防止并发覆盖
	Version int64 `gorm:"default:1"`

	UpdatedAt time.Time
}

// CommitModel 是 core.Commit 在关系型数据库中的投影 (索引)
// 用于快速查询历史 (tv log)，支持按作者、时间、元数据搜索
// 注意：为了避免跟 core.Commit 混淆，我们叫它 CommitModel
type CommitModel struct {
	// Hash 是主键 (Merkle Root)
	Hash string `gorm:"primaryKey;type:char(64)"`

	// 基础元数据 (B-Tree 索引，适合排序和精确查找)
	Author    string `gorm:"index;type:varchar(100)"`
	Message   string `gorm:"type:text"`
	Timestamp int64  `gorm:"index"` // 使用 int64 存时间戳，方便范围查询

	// 树结构指针
	TreeHash string `gorm:"type:char(64);not null"`

	// --- AI Infra 核心特性 ---

	// Parents: 使用 JSONB 存储父节点列表 (支持 Merge Commit 多父节点)
	// 这是一个数组 ["hash1", "hash2"]
	//为了测试,目前不用jsonb
	//TODO: 未来调整测试,改回 jsonb,以及gin
	Parents datatypes.JSON

	// Meta: 存储 AI 训练超参数、Metrics、Tags 等非结构化数据
	// 关键：使用 GIN 索引 (type:gin) 支持 {"accuracy": 0.9} 这种任意字段的毫秒级检索
	Meta datatypes.JSON `gorm:"index:idx_commit_meta"`

	CreatedAt time.Time
}

// TableName 强制指定表名
func (CommitModel) TableName() string {
	return "commits"
}
