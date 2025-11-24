package core

import (
	"encoding/hex"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// Link 代表 Merkle DAG 中的一条边 (指向子节点的哈希引用)
// 在 Go 层面，它只是一个包装了 Hash 字符串的结构体
// 在 CBOR 层面，它会被序列化为 Tag 42(0x00 + HashBytes)
type Link struct {
	Hash string
}

const (
	linkTagNumber = 42
)

// NewLink 辅助函数
func NewLink(hash string) Link {
	return Link{Hash: hash}
}

// MarshalCBOR 实现自定义序列化逻辑
// 规范：Tag 42, Content = [0x00, byte1, byte2...]
func (l Link) MarshalCBOR() ([]byte, error) {
	// 1. 解码 Hex 字符串
	hashBytes, err := hex.DecodeString(l.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid hash format in link: %w", err)
	}

	// 2. 添加 Multibase Identity 前缀 (0x00)
	// 这是 IPFS CIDv1 的要求，表示后面紧跟的是原始哈希
	cidBytes := append([]byte{0x00}, hashBytes...)

	// 3. 包装为 Tag 42
	// cbor.Tag 会被库自动处理为 Major Type 6
	return em.Marshal(cbor.Tag{
		Number:  linkTagNumber,
		Content: cidBytes,
	})
}

// UnmarshalCBOR 实现自定义反序列化逻辑
func (l *Link) UnmarshalCBOR(data []byte) error {
	var tag cbor.Tag
	// 使用我们配置好的 strict decoder (dm) 进行解码
	if err := dm.Unmarshal(data, &tag); err != nil {
		return err
	}

	// 1. 校验 Tag Number
	if tag.Number != linkTagNumber {
		return fmt.Errorf("expected tag 42 for Link, got %d", tag.Number)
	}

	// 2. 获取内容字节
	bytes, ok := tag.Content.([]byte)
	if !ok {
		return fmt.Errorf("link content must be byte string")
	}

	// 3. 严格校验 Multibase 前缀
	if len(bytes) < 1 {
		return fmt.Errorf("invalid link: empty content")
	}
	if bytes[0] != 0x00 {
		return fmt.Errorf("invalid link: missing 0x00 multibase prefix")
	}

	// 4. 还原 Hash (去掉前缀)
	l.Hash = hex.EncodeToString(bytes[1:])
	return nil
}
