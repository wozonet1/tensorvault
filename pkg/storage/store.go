package storage

import (
	"context"
	"errors"
	"io"

	"tensorvault/pkg/core"
)

var (
	ErrNotFound = errors.New("object not found")
)

// Store defines the interface for a storage backend.
// Implementations can be local disk, cloud storage, or in-memory storage.
type Store interface {
	// Put 将一个核心对象持久化
	// 它不需要返回 Hash，因为 Hash 已经在 core.Object 里了
	Put(ctx context.Context, obj core.Object) error

	// Get 根据 Hash 读取原始数据
	// 注意：这里返回的是 io.ReadCloser 而不是 []byte
	// 原因：为了支持大文件的流式读取 (Stream)，避免一次性把 100MB 读进内存
	Get(ctx context.Context, hash string) (io.ReadCloser, error)

	// Has 检查对象是否存在 (用于去重逻辑)
	Has(ctx context.Context, hash string) (bool, error)

	// Delete (可选，MVP 阶段可以先不实现，因为 CAS 通常只增不删)
	// Delete(ctx context.Context, hash string) error
}
