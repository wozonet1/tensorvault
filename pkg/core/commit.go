package core

import (
	"tensorvault/pkg/types"
	"time"
)

type Commit struct {
	hash     types.Hash `cbor:"-"`
	rawBytes []byte     `cbor:"-"`

	TypeVal ObjectType `cbor:"t"`

	// CHANGE: 使用 Link 类型
	TreeCid Link   `cbor:"th"`
	Parents []Link `cbor:"p"`

	Author  string `cbor:"a"`
	Message string `cbor:"m"`

	// CHANGE: 使用 int64 明确时间戳类型
	Timestamp int64 `cbor:"ts"`
}

func NewCommit(treeHash types.Hash, parents []types.Hash, author, msg string) (*Commit, error) {
	// 转换 parents string -> Link
	parentLinks := make([]Link, len(parents))
	for i, p := range parents {
		parentLinks[i] = NewLink(p)
	}

	c := &Commit{
		TypeVal:   TypeCommit,
		TreeCid:   NewLink(treeHash), // 转换
		Parents:   parentLinks,
		Author:    author,
		Message:   msg,
		Timestamp: time.Now().Unix(), // 使用 Unix 时间戳
	}

	h, b, err := CalculateHash(c)
	if err != nil {
		return nil, err
	}
	c.hash = h
	c.rawBytes = b
	return c, nil
}

func (c *Commit) Type() ObjectType { return TypeCommit }
func (c *Commit) ID() types.Hash   { return c.hash }
func (c *Commit) Bytes() []byte    { return c.rawBytes }
