// pkg/types/common.go
package types

// Hash 代表对象的唯一标识符 (SHA256 Hex String)
// 这是一个“值对象”，应当是不可变的。
type Hash string

// 为什么好？可以绑定方法
func (h Hash) String() string { return string(h) }

// 验证 Hash 合法性
func (h Hash) IsZero() bool  { return h == "" }
func (h Hash) IsValid() bool { return len(h) == 64 } // 简单的长度检查

type HashPrefix string

type RepoPath string
