package refs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefFlow(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	// 1. 初始状态应该是 NoHead
	_, err := mgr.GetHead()
	assert.ErrorIs(t, err, ErrNoHead)

	// 2. 更新 HEAD
	hash := "bafy-commit-hash-123"
	err = mgr.UpdateHead(hash)
	require.NoError(t, err)

	// 3. 读取 HEAD
	readHash, err := mgr.GetHead()
	require.NoError(t, err)
	assert.Equal(t, hash, readHash)
}
