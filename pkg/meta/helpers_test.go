package meta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"tensorvault/pkg/core"
	"tensorvault/pkg/types"

	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// 通用辅助函数 (Helpers)
// 注意：文件名必须以 _test.go 结尾，否则会被编译进生产代码！
// -----------------------------------------------------------------------------

// mockHash 生成合法的测试用 Hash
func mockHash(input string) types.Hash {
	sum := sha256.Sum256([]byte(input))
	return types.Hash(hex.EncodeToString(sum[:]))
}

// mustNewCommit 创建 Commit，如果失败直接终止测试
// 这让主测试代码极其干净
func mustNewCommit(t *testing.T, treeHash types.Hash, parents []types.Hash, author, msg string, msgAndArgs ...any) *core.Commit {
	t.Helper()
	c, err := core.NewCommit(treeHash, parents, author, msg)
	require.NoError(t, err, msgAndArgs...) // 透传消息
	return c
}

// mustIndexCommit 强制索引 Commit，失败则终止
func mustIndexCommit(t *testing.T, repo *Repository, c *core.Commit, msgAndArgs ...any) {
	t.Helper() // 关键：报错时回溯栈帧
	err := repo.IndexCommit(context.Background(), c)

	// 技巧：如果调用者没传消息，给个默认的；传了就用调用者的
	// require.NoError 支持可变参数，直接透传即可
	require.NoError(t, err, msgAndArgs...)
}

// mustUpdateRef 强制更新引用，失败则终止
// 适用于 Happy Path (预期成功的场景)
func mustUpdateRef(t *testing.T, repo *Repository, name string, newHash types.Hash, oldVersion int64, msgAndArgs ...any) {
	t.Helper()
	// 注意：底层接受的是 string 类型的 hash，这里我们需要转一下或者保持接口一致
	// 假设 UpdateRef 签名是 string，我们这里传入 types.Hash 需要 .String()
	err := repo.UpdateRef(context.Background(), name, newHash, oldVersion)
	require.NoError(t, err, msgAndArgs...)
}
