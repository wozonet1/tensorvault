package core

import (
	"encoding/hex"
	"tensorvault/pkg/types"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	badPrefixHex := string("d82a5820" + mockHash("bad"))
	badPrefixBytes, err := hex.DecodeString(badPrefixHex)
	require.NoError(t, err)
	var l Link
	err = l.UnmarshalCBOR(badPrefixBytes)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing 0x00 multibase prefix")

	// Case B: 错误的 Tag (不是 42)
	// Tag 43 (d82b) ...
	wrongTagHex := string("d82b582100" + mockHash("wrong"))
	wrongTagBytes, err := hex.DecodeString(wrongTagHex)
	require.NoError(t, err)
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
	c := mustNewCommit(t,
		mockHash("tree_root"), // 必须是合法 Hex
		[]types.Hash{mockHash("parent1"), mockHash("parent2")}, // 必须是合法 Hex
		"author_test",
		"message_test",
	)

	// 第一次计算哈希
	hash1, bytes1 := mustCalculateHash(t, c)

	// 反序列化回来
	var c2 Commit
	err := DecodeObject(bytes1, &c2)
	require.NoError(t, err)

	// 再次计算哈希
	hash2, _ := mustCalculateHash(t, &c2)

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
	c := mustNewCommit(t, mockHash("tree"), nil, "me", "msg")

	// 手动检查时间戳
	assert.Greater(t, c.Timestamp, int64(1700000000))
	assert.Less(t, c.Timestamp, time.Now().Unix()+100)

}

// -----------------------------------------------------------------------------
// 1. 基础哈希计算测试 (覆盖 CalculateBlobHash)
// -----------------------------------------------------------------------------

func TestCalculateBlobHash(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantHash string // 预先计算好的 SHA256，用于回归测试
	}{
		{
			name:     "Empty input",
			input:    []byte(""),
			wantHash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "Hello World",
			input:    []byte("hello world"),
			wantHash: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateBlobHash(tt.input)

			// 1. 验证 Hash 值的正确性 (Regression Test)
			assert.Equal(t, tt.wantHash, got.String())

			// 2. 验证 Hash 格式的合法性 (Domain Rule)
			assert.True(t, got.IsValid(), "Hash must be valid")
		})
	}
}

// -----------------------------------------------------------------------------
// 2. 对象接口兼容性测试 (Compile-time Check)
// -----------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	// 这是一个编译期检查，确保所有类型都实现了 Object 接口
	// 如果没实现，这里会编译报错
	var _ Object = &Chunk{}
	var _ Object = &FileNode{}
	var _ Object = &Tree{}
	var _ Object = &Commit{}
}

// -----------------------------------------------------------------------------
// 3. 序列化闭环测试 (Round-Trip)
// -----------------------------------------------------------------------------

func TestObject_RoundTrip_Chunk(t *testing.T) {
	data := []byte("some random binary data")
	chunk := NewChunk(data)

	// 验证类型
	assert.Equal(t, TypeChunk, chunk.Type())
	assert.Equal(t, int64(len(data)), chunk.Size())

	// Chunk 本身不走 CBOR 序列化，它的 Bytes() 就是 raw data
	assert.Equal(t, data, chunk.Bytes())
}

func TestObject_RoundTrip_Tree(t *testing.T) {
	// 构造一个 Tree
	entries := []TreeEntry{
		NewFileEntry("file1.txt", "1111111111111111111111111111111111111111111111111111111111111111", 100),
		NewDirEntry("subdir", "2222222222222222222222222222222222222222222222222222222222222222"),
	}

	// 为了测试 Deterministic (确定性)，我们故意乱序插入（虽然 NewTree 内部应该排序，但 TreeEntry slice 本身顺序取决于构造者）
	// 注意：我们的 Tree 实现目前假设 entries 传入时是什么样就是什么样，或者 treebuilder 会排序
	// 这里主要测试序列化和反序列化能不能还原

	originalTree, err := NewTree(entries)
	require.NoError(t, err)

	// 1. 序列化
	encoded := originalTree.Bytes()

	// 2. 反序列化
	var restoredTree Tree
	err = DecodeObject(encoded, &restoredTree)
	require.NoError(t, err)
	restoredHash, _ := mustCalculateHash(t, &restoredTree)
	// 3. 比对
	assert.Equal(t, originalTree.ID(), restoredHash, "Hash should be recalculated or matched")
	assert.Equal(t, len(entries), len(restoredTree.Entries))
	assert.Equal(t, "file1.txt", restoredTree.Entries[0].Name)
	assert.Equal(t, types.Hash("1111111111111111111111111111111111111111111111111111111111111111"), restoredTree.Entries[0].Cid.Hash)
}

func TestObject_RoundTrip_Commit(t *testing.T) {
	// 构造 Commit
	treeHash := types.Hash("3333333333333333333333333333333333333333333333333333333333333333")
	parents := []types.Hash{
		"4444444444444444444444444444444444444444444444444444444444444444",
	}

	originalCommit, err := NewCommit(treeHash, parents, "Tester", "Test Message")
	require.NoError(t, err)

	// 1. 序列化
	encoded := originalCommit.Bytes()

	// 2. 反序列化
	var restoredCommit Commit
	err = DecodeObject(encoded, &restoredCommit)
	require.NoError(t, err)

	// 3. 比对字段
	assert.Equal(t, "Tester", restoredCommit.Author)
	assert.Equal(t, "Test Message", restoredCommit.Message)
	assert.Equal(t, treeHash, restoredCommit.TreeCid.Hash)
	assert.Equal(t, parents[0], restoredCommit.Parents[0].Hash)

	// 4. 验证时间戳 (CBOR 应该能还原 int64)
	assert.Equal(t, originalCommit.Timestamp, restoredCommit.Timestamp)
}

// TestCanonical_Sort 验证 Map Key 的排序是否生效
// 这是 Merkle DAG 确定性的关键
func TestCanonical_Sort(t *testing.T) {
	// 我们手动构造两个字段顺序不同的 map，序列化后应该得到相同的字节流
	// 注意：Go 的 map 遍历是随机的，所以我们通过 cbor 库的配置来保证

	// 这里我们可以测试 NewCommit 是否对同样的输入产生同样的 Hash
	// 即使我们在内存里怎么折腾

	c1 := mustNewCommit(t, mockHash("hash1"), nil, "A", "Msg")
	time.Sleep(1 * time.Second) // 改变时间
	c2 := mustNewCommit(t, mockHash("hash1"), nil, "A", "Msg")

	// 因为时间变了，Hash 肯定变
	assert.NotEqual(t, c1.ID(), c2.ID())

	// 手动强制时间一致
	fixedTime := int64(100000)
	c1.Timestamp = fixedTime
	c2.Timestamp = fixedTime

	// 重新计算 Hash (模拟)
	// 注意：core 包目前没有暴露出 Rehash 的方法，NewCommit 生成后 rawBytes 就固定了
	// 所以我们得用 CalculateHash 测

	hash1, bytes1 := mustCalculateHash(t, c1)
	hash2, bytes2 := mustCalculateHash(t, c2)

	assert.Equal(t, hash1, hash2, "Identical content must yield identical hash")
	assert.Equal(t, bytes1, bytes2, "Identical content must yield identical bytes")
}
