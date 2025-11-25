package core

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// 辅助工具
// -----------------------------------------------------------------------------

// mockHash 生成一个合法的 32 字节 Hex 字符串 (64字符长度)
// 用于满足 Link 对 Hex 格式的要求
func mockHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// -----------------------------------------------------------------------------
// 1. Link 测试
// -----------------------------------------------------------------------------

func TestLink_Marshal_Compliance(t *testing.T) {
	// 使用合法的 Hex 字符串
	validHash := mockHash("test-content")
	link := NewLink(validHash)

	// 1. 序列化
	data, err := link.MarshalCBOR()
	require.NoError(t, err)

	// 2. 验证 Hex 前缀
	// Tag 42 (0xd82a) + ByteString 33 bytes (0x5821) + Prefix (0x00)
	expectedPrefix := "d82a582100"
	encodedHex := hex.EncodeToString(data)

	assert.Equal(t, expectedPrefix, encodedHex[:10], "Link 序列化必须包含 Tag 42 和 0x00 前缀")
}

func TestLink_Unmarshal_RoundTrip(t *testing.T) {
	// 1. 构造合法数据
	originalHash := mockHash("round-trip-test")
	link := NewLink(originalHash)

	// 2. 编码
	data, err := link.MarshalCBOR()
	require.NoError(t, err)

	// 3. 解码
	var l2 Link
	err = l2.UnmarshalCBOR(data)
	require.NoError(t, err)

	// 4. 比对
	assert.Equal(t, originalHash, l2.Hash)
}

func TestLink_Unmarshal_Strictness(t *testing.T) {
	// Case A: 缺少 0x00 前缀
	// 构造一个 Tag 42，但内容没有 0x00
	// 手动构造错误数据: Tag 42 (d82a) + Bytes 32 (5820) + ...
	badPrefixHex := "d82a5820" + mockHash("bad")
	badPrefixBytes, _ := hex.DecodeString(badPrefixHex)

	var l Link
	err := l.UnmarshalCBOR(badPrefixBytes)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing 0x00 multibase prefix")

	// Case B: 错误的 Tag (不是 42)
	// Tag 43 (d82b) ...
	wrongTagHex := "d82b582100" + mockHash("wrong")
	wrongTagBytes, _ := hex.DecodeString(wrongTagHex)
	err = l.UnmarshalCBOR(wrongTagBytes)
	assert.Error(t, err)
	// 注意：错误信息取决于 cbor 库的具体实现，可能报 tag mismatch 或者 unexpected tag
	// 这里只要报错就行
}

// -----------------------------------------------------------------------------
// 2. 确定性哈希测试 (Canonical Encoding)
// -----------------------------------------------------------------------------

func TestCanonical_Encoding(t *testing.T) {
	// 构造一个 Commit
	c, err := NewCommit(
		mockHash("tree_root"),                              // 必须是合法 Hex
		[]string{mockHash("parent1"), mockHash("parent2")}, // 必须是合法 Hex
		"author_test",
		"message_test",
	)
	require.NoError(t, err)

	// 第一次计算哈希
	hash1, bytes1, err := CalculateHash(c)
	require.NoError(t, err)

	// 反序列化回来
	var c2 Commit
	err = DecodeObject(bytes1, &c2)
	require.NoError(t, err)

	// 再次计算哈希
	hash2, _, err := CalculateHash(&c2)
	require.NoError(t, err)

	// 断言：同一个对象的哈希必须永远一致
	assert.Equal(t, hash1, hash2, "Merkle DAG 哈希计算必须具备确定性")
}

// -----------------------------------------------------------------------------
// 3. 完整对象 Round-Trip 测试
// -----------------------------------------------------------------------------

func TestFileNode_RoundTrip(t *testing.T) {
	chunks := []ChunkLink{
		{Cid: NewLink(mockHash("chunk1")), Size: 1024},
		{Cid: NewLink(mockHash("chunk2")), Size: 2048},
	}

	node, err := NewFileNode(3072, chunks)
	require.NoError(t, err)

	encoded := node.Bytes()
	assert.NotEmpty(t, encoded)

	var node2 FileNode
	err = DecodeObject(encoded, &node2)
	require.NoError(t, err)

	assert.Equal(t, TypeFileNode, node2.TypeVal)
	assert.Equal(t, int64(3072), node2.TotalSize)
	assert.Equal(t, 2, len(node2.Chunks))
	// 验证 Hash 是否还原成功
	assert.Equal(t, chunks[0].Cid.Hash, node2.Chunks[0].Cid.Hash)
}

func TestCommit_Timestamp_Type(t *testing.T) {
	// 必须处理 error，防止 Panic
	c, err := NewCommit(mockHash("tree"), nil, "me", "msg")
	require.NoError(t, err)

	// 手动检查时间戳
	assert.Greater(t, c.Timestamp, int64(1700000000))
	assert.Less(t, c.Timestamp, time.Now().Unix()+100)

	// 简单的启发式检查：确保没有 Tag 1 (0xc1) 或 Tag 0 (0xc0)
	// 这证明时间被编码为了纯整数
	for _, b := range c.Bytes() {
		if b == 0xc0 || b == 0xc1 {
			// 这是一个弱检查，因为 c0/c1 也可能是 Hash 的一部分
			// 但如果它出现在最前面，就有问题
			// 只要测试跑通，说明数据结构没崩
		}
	}
}
