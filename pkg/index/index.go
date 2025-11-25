// pkg/index/index.go
package index

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Entry 代表暂存区中的一条记录
type Entry struct {
	Path       string    `json:"path"`        // 相对路径 (如 "data/model.bin")
	Hash       string    `json:"hash"`        // FileNode 的 Hash (Merkle Root)
	Size       int64     `json:"size"`        // 文件大小
	ModifiedAt time.Time `json:"modified_at"` // 修改时间
}

// Index 管理暂存区状态
type Index struct {
	path    string           // 物理文件路径 (.tv/index)
	Entries map[string]Entry `json:"entries"`
	mu      sync.RWMutex
}

// NewIndex 加载或创建一个新的 Index
func NewIndex(indexPath string) (*Index, error) {
	idx := &Index{
		path:    indexPath,
		Entries: make(map[string]Entry),
	}

	// 尝试加载现有文件
	if _, err := os.Stat(indexPath); err == nil {
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read index: %w", err)
		}
		if err := json.Unmarshal(data, idx); err != nil {
			return nil, fmt.Errorf("corrupted index file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	return idx, nil
}

// Add 更新一条记录
func (i *Index) Add(path string, hash string, size int64) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.Entries[path] = Entry{
		Path:       path,
		Hash:       hash,
		Size:       size,
		ModifiedAt: time.Now(),
	}
}

// Save 将暂存区持久化到磁盘
func (i *Index) Save() error {
	i.mu.RLock()
	defer i.mu.RUnlock()

	// 格式化输出 (Indented)，符合您的“可观测性”要求
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(i.path, data, 0644)
}
