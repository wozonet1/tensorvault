package core

import (
	"crypto/sha256"
	"encoding/hex"
	"tensorvault/pkg/types"
	"testing"

	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// 辅助工具
// -----------------------------------------------------------------------------

// mockHash 生成一个合法的 32 字节 Hex 字符串 (64字符长度)
// 用于满足 Link 对 Hex 格式的要求
func mockHash(input string) types.Hash {
	sum := sha256.Sum256([]byte(input))
	return types.Hash(hex.EncodeToString(sum[:]))
}

// mustNewCommit 创建 Commit，如果失败直接终止测试
// 这让主测试代码极其干净
func mustNewCommit(t *testing.T, treeHash types.Hash, parents []types.Hash, author, msg string, msgAndArgs ...any) *Commit {
	t.Helper()
	c, err := NewCommit(treeHash, parents, author, msg)
	require.NoError(t, err, msgAndArgs...) // 透传消息
	return c
}

func mustCalculateHash(t *testing.T, obj Object, msgAndArgs ...any) (types.Hash, []byte) {
	t.Helper()
	h, bytes, err := CalculateHash(obj)
	require.NoError(t, err, msgAndArgs...)
	return h, bytes
}
