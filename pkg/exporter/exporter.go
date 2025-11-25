package exporter

import (
	"context"
	"fmt"
	"io"

	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"
)

type Exporter struct {
	store storage.Store
}

func NewExporter(store storage.Store) *Exporter {
	return &Exporter{store: store}
}

// ExportFile 根据 FileNode 的 Hash，将还原的文件写入 writer
func (e *Exporter) ExportFile(ctx context.Context, hash string, writer io.Writer) error {
	// 1. 获取 FileNode 元数据
	// 注意：storage.Get 返回的是 io.ReadCloser，我们需要读出来反序列化
	nodeReader, err := e.store.Get(ctx, hash)
	if err != nil {
		return fmt.Errorf("failed to get filenode meta: %w", err)
	}
	defer nodeReader.Close()

	nodeBytes, err := io.ReadAll(nodeReader)
	if err != nil {
		return fmt.Errorf("failed to read filenode bytes: %w", err)
	}

	// 2. 反序列化 FileNode
	var fileNode core.FileNode
	if err := core.DecodeObject(nodeBytes, &fileNode); err != nil {
		return fmt.Errorf("failed to decode filenode: %w", err)
	}

	// 3. 类型防御 (Defensive Check)
	if fileNode.TypeVal != core.TypeFileNode {
		return fmt.Errorf("object is not a filenode, got: %s", fileNode.TypeVal)
	}

	// 4. 遍历所有 Chunk，按顺序写入 Writer (Reassembly)
	for i, chunkLink := range fileNode.Chunks {
		// 【技巧】使用匿名函数构建一个 Scope
		err := func() error {
			// 获取 Chunk 数据
			// (注意这里用了重构后的 Cid 命名)
			chunkReader, err := e.store.Get(ctx, chunkLink.Cid.Hash)
			if err != nil {
				return fmt.Errorf("failed to get chunk %d: %w", i, err)
			}
			// ✅ 安全：函数返回时立即关闭，不会堆积句柄
			defer chunkReader.Close()

			// 流式拷贝
			if _, err := io.Copy(writer, chunkReader); err != nil {
				return fmt.Errorf("failed to write chunk %d data: %w", i, err)
			}
			return nil
		}()

		// 处理匿名函数返回的错误
		if err != nil {
			return err
		}
	}

	return nil
}
