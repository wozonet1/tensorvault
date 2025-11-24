package core

import "fmt"

type EntryType string

const (
	EntryFile EntryType = "file"
	EntryDir  EntryType = "dir"
)

type TreeEntry struct {
	Name string    `cbor:"n"`
	Type EntryType `cbor:"t"`
	Hash Link      `cbor:"h"`
	Size int64     `cbor:"s"`
}

type Tree struct {
	hash     string `cbor:"-"`
	rawBytes []byte `cbor:"-"`

	TypeVal ObjectType  `cbor:"t"`
	Entries []TreeEntry `cbor:"e"`
}

// NewTree 创建一个新的目录树节点
func NewTree(entries []TreeEntry) (*Tree, error) {
	t := &Tree{
		TypeVal: TypeTree,
		Entries: entries,
	}
	h, b, err := CalculateHash(t)
	if err != nil {
		return nil, err
	}
	t.hash = h
	t.rawBytes = b
	return t, nil
}

// NewTreeEntryFromObject 自动根据子对象生成条目
func NewTreeEntryFromObject(name string, child Object) (TreeEntry, error) {
	var entryType EntryType
	var size int64

	switch n := child.(type) { // 使用 Type Switch
	case *FileNode:
		entryType = EntryFile
		size = n.TotalSize // 只有文件有逻辑大小
	case *Chunk:
		entryType = EntryFile // 这种情况比较少见，但也兼容
		size = n.Size()
	case *Tree:
		entryType = EntryDir
		size = 0 // 目录大小通常记为 0，或者你可以存子节点数量
	case *Commit:
		// Commit 通常不会作为 Tree 的子节点，这在逻辑上可能是不合法的
		return TreeEntry{}, fmt.Errorf("commit cannot be an entry inside a tree")
	default:
		return TreeEntry{}, fmt.Errorf("unsupported object type: %s", child.Type())
	}

	return TreeEntry{
		Name: name,
		Type: entryType,
		Hash: NewLink(child.ID()),
		Size: size,
	}, nil
}

func (t *Tree) Type() ObjectType { return TypeTree }
func (t *Tree) ID() string       { return t.hash }
func (t *Tree) Bytes() []byte    { return t.rawBytes }
