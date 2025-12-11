// pkg/types/common.go
package types

// Hash 代表对象的唯一标识符 (SHA256 Hex String)
// 这是一个“值对象”，应当是不可变的。
type Hash string

func (h Hash) String() string { return string(h) }

// 验证 Hash 合法性
func (h Hash) IsZero() bool  { return h == "" }
func (h Hash) IsValid() bool { return len(h) == 64 } // 简单的长度检查

type LinearHash string

func (h LinearHash) String() string { return string(h) }
func (h LinearHash) IsValid() bool  { return len(h) == 64 }

// 辅助转换 (显式转换，提醒开发者注意)
func (h LinearHash) ToHash() Hash { return Hash(h) }

type HashPrefix string

func (p HashPrefix) String() string { return string(p) }

type RepoPath string
