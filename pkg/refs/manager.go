package refs

import (
	"context"
	"errors"
	"fmt"

	"tensorvault/pkg/meta"
	"tensorvault/pkg/types"
)

var (
	// ErrNoHead 当仓库是新的（还没有 HEAD 记录）时返回
	ErrNoHead = errors.New("HEAD not found (clean repo)")

	// ErrStaleHead 当尝试更新 HEAD 但版本号不匹配时返回（并发冲突）
	ErrStaleHead = errors.New("HEAD has changed since you last read it")
)

// Manager 负责管理引用 (Refs)
// Phase 3: 底层由本地文件改为 PostgreSQL
type Manager struct {
	repo *meta.Repository
}

func NewManager(repo *meta.Repository) *Manager {
	return &Manager{repo: repo}
}

// GetHead 读取当前 HEAD 的 Hash 和 版本号
// 返回: (hash, version, error)
func (m *Manager) GetHead(ctx context.Context) (types.Hash, int64, error) {
	ref, err := m.repo.GetRef(ctx, "HEAD")
	if err != nil {
		if errors.Is(err, meta.ErrRefNotFound) {
			return "", 0, ErrNoHead
		}
		return "", 0, fmt.Errorf("failed to get HEAD: %w", err)
	}
	return ref.CommitHash, ref.Version, nil
}

// UpdateHead 原子更新 HEAD
// 必须提供 oldVersion 以进行乐观锁检查 (CAS)
// 如果是第一次提交，oldVersion 传 0
func (m *Manager) UpdateHead(ctx context.Context, newHash types.Hash, oldVersion int64) error {
	err := m.repo.UpdateRef(ctx, "HEAD", newHash, oldVersion)
	if err != nil {
		if errors.Is(err, meta.ErrConcurrentUpdate) {
			return ErrStaleHead
		}
		return fmt.Errorf("failed to update HEAD: %w", err)
	}
	return nil
}
