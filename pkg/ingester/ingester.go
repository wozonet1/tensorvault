package ingester

import (
	"context"
	"fmt"
	"io"

	"tensorvault/pkg/chunker"
	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"
)

type Ingester struct {
	store   storage.Store
	chunker *chunker.Chunker
}

func NewIngester(store storage.Store) *Ingester {
	return &Ingester{
		store:   store,
		chunker: chunker.NewChunker(),
	}
}

// IngestFile 读取一个文件流，切分，存储，并返回 FileNode 的 Hash
func (ing *Ingester) IngestFile(ctx context.Context, reader io.Reader) (*core.FileNode, error) {
	// 1. 读取全部数据 (MVP 阶段：先读进内存，Phase 3 再做流式优化)
	// 警告：这不适合 10GB 文件，但适合跑通逻辑
	//TODO: 实现流式切分和存储
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	builder := core.NewFileNodeBuilder()
	// 2. 切分
	cutPoints := ing.chunker.Cut(data)

	start := 0

	// 3. 遍历切点，创建 Chunk 对象
	for _, end := range cutPoints {
		chunkData := data[start:end]
		chunkObj := core.NewChunk(chunkData) // 创建 L1 Chunk

		// 4. 存储 Chunk (存入磁盘)
		if err := ing.store.Put(ctx, chunkObj); err != nil {
			return nil, fmt.Errorf("failed to store chunk: %w", err)
		}

		builder.Add(chunkObj)
		start = end
	}

	// 6. 创建 FileNode (L2)
	fileNode, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create file node: %w", err)
	}

	// 7. 存储 FileNode
	if err := ing.store.Put(ctx, fileNode); err != nil {
		return nil, fmt.Errorf("failed to store file node: %w", err)
	}

	return fileNode, nil
}
