package refs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNoHead = errors.New("HEAD not found (clean repo)")

// Manager 负责管理引用 (Refs)，目前主要是 HEAD
type Manager struct {
	rootPath string
}

func NewManager(rootPath string) *Manager {
	return &Manager{rootPath: rootPath}
}

// headPath 返回 .tv/HEAD 的物理路径
func (m *Manager) headPath() string {
	return filepath.Join(m.rootPath, "HEAD")
}

// GetHead 读取当前的 Commit Hash
// 如果是新仓库（没提交过），返回 ErrNoHead
func (m *Manager) GetHead() (string, error) {
	data, err := os.ReadFile(m.headPath())
	if os.IsNotExist(err) {
		return "", ErrNoHead
	}
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}

	// 清理换行符 (vim 编辑时可能会自动加 \n)
	return strings.TrimSpace(string(data)), nil
}

// UpdateHead 更新 HEAD 到新的 Commit Hash
func (m *Manager) UpdateHead(commitHash string) error {
	// 简单的原子写逻辑：直接覆盖
	// 生产环境可能需要文件锁，MVP 暂略
	return os.WriteFile(m.headPath(), []byte(commitHash), 0644)
}
