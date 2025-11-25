package disk

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"
)

// Adapter 实现了 storage.Store 接口
type Adapter struct {
	rootPath string // 比如: /home/user/.tv/objects
}

// NewAdapter 创建一个新的磁盘存储适配器
func NewAdapter(root string) (*Adapter, error) {
	// 确保根目录存在
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root storage dir: %w", err)
	}
	return &Adapter{rootPath: root}, nil
}

// layout 返回哈希对应的物理路径
// 策略：使用前 2 个字符作为子目录 (Sharding)
// Example: hash "aabbcc..." -> root/aa/bbcc...
func (s *Adapter) layout(hash string) string {
	if len(hash) < 2 {
		return filepath.Join(s.rootPath, hash)
	}
	return filepath.Join(s.rootPath, hash[:2], hash[2:])
}

func (s *Adapter) Put(ctx context.Context, obj core.Object) error {
	hash := obj.ID()
	targetPath := s.layout(hash)

	// 1. 检查是否存在 (幂等性)
	if _, err := os.Stat(targetPath); err == nil {
		return nil // 已经存在，直接跳过 (CAS 的好处)
	}

	// 2. 准备目录
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// 3. 原子写入 (Atomic Write)
	// 技巧：先写到一个临时文件，然后 Rename。
	// 这样保证要么文件不存在，要么文件是完整的。
	tempFile, err := os.CreateTemp(dir, "temp-*")
	if err != nil {
		return err
	}
	// 确保临时文件会被清理（如果成功 Rename 了，这个删除会失效，或者无害）
	defer os.Remove(tempFile.Name())

	// 写入数据
	data := obj.Bytes()
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}
	tempFile.Close() // 必须先关闭才能 Rename

	// 4. 移动到最终位置
	if err := os.Rename(tempFile.Name(), targetPath); err != nil {
		return err
	}

	return nil
}

func (s *Adapter) Get(ctx context.Context, hash string) (io.ReadCloser, error) {
	targetPath := s.layout(hash)

	f, err := os.Open(targetPath)
	if os.IsNotExist(err) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Adapter) Has(ctx context.Context, hash string) (bool, error) {
	targetPath := s.layout(hash)
	_, err := os.Stat(targetPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
