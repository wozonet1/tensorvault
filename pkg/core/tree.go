package core

import "tensorvault/pkg/types"

type EntryType string

const (
	EntryFile EntryType = "file"
	EntryDir  EntryType = "dir"
)

type TreeEntry struct {
	Name string    `cbor:"n"`
	Type EntryType `cbor:"t"`
	Cid  Link      `cbor:"h"`
	Size int64     `cbor:"s"`
}

type Tree struct {
	hash     types.Hash `cbor:"-"`
	rawBytes []byte     `cbor:"-"`

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

// NewFileEntry 创建一个文件类型的目录项
// 它封装了 Link 的创建逻辑
func NewFileEntry(name string, hash types.Hash, size int64) TreeEntry {
	return TreeEntry{
		Name: name,
		Type: EntryFile,
		Cid:  NewLink(hash), // 自动封装 Link
		Size: size,
	}
}

// NewDirEntry 创建一个目录类型的目录项
// 强制规定目录大小为 0 (或者未来你可以改为累加大小，只需改这里一处)
func NewDirEntry(name string, hash types.Hash) TreeEntry {
	return TreeEntry{
		Name: name,
		Type: EntryDir,
		Cid:  NewLink(hash),
		Size: 0,
	}
}

func (t *Tree) Type() ObjectType { return TypeTree }
func (t *Tree) ID() types.Hash   { return t.hash }
func (t *Tree) Bytes() []byte    { return t.rawBytes }
