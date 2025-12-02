package exporter

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/types"

	"golang.org/x/sync/errgroup"
)

const (
	RestoreWorkerCount = 16 // 并发恢复的 Worker 数量
)

type Exporter struct {
	store storage.Store
}

func NewExporter(store storage.Store) *Exporter {
	return &Exporter{store: store}
}

// ExportFile 智能导出文件
// 如果 writer 支持 io.WriterAt (如 *os.File)，则使用并发下载 (Parallel Restore)
// 否则 (如 os.Stdout)，回退到串行流式下载 (Serial Restore)
func (e *Exporter) ExportFile(ctx context.Context, hash types.Hash, writer io.Writer) error {
	// 1. 获取并解析 FileNode
	nodeReader, err := e.store.Get(ctx, hash)
	if err != nil {
		return fmt.Errorf("failed to get filenode meta: %w", err)
	}
	defer nodeReader.Close()

	nodeBytes, err := io.ReadAll(nodeReader)
	if err != nil {
		return fmt.Errorf("failed to read filenode bytes: %w", err)
	}

	var fileNode core.FileNode
	if err := core.DecodeObject(nodeBytes, &fileNode); err != nil {
		return fmt.Errorf("failed to decode filenode: %w", err)
	}

	if fileNode.TypeVal != core.TypeFileNode {
		return fmt.Errorf("object is not a filenode, got: %s", fileNode.TypeVal)
	}

	// 2. 策略分发
	// 检查 writer 是否支持“随机写入” (WriteAt)
	if wAt, ok := writer.(io.WriterAt); ok {
		// 🚀 路径 A: 并发恢复 (适用于 Checkout 到本地文件)
		return e.exportFileConcurrent(ctx, &fileNode, wAt)
	}

	// 🐌 路径 B: 串行恢复 (适用于 Cat 到标准输出)
	return e.exportFileSerial(ctx, &fileNode, writer)
}

// exportFileSerial 传统的串行流式实现
func (e *Exporter) exportFileSerial(ctx context.Context, fileNode *core.FileNode, writer io.Writer) error {
	for i, chunkLink := range fileNode.Chunks {
		err := func() error {
			rc, err := e.store.Get(ctx, chunkLink.Cid.Hash)
			if err != nil {
				return fmt.Errorf("failed to get chunk %d: %w", i, err)
			}
			defer rc.Close()

			if _, err := io.Copy(writer, rc); err != nil {
				return fmt.Errorf("failed to write chunk %d: %w", i, err)
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

// restoreJob 并发任务结构
type restoreJob struct {
	hash   types.Hash
	offset int64 // 写入文件的绝对偏移量
	size   int   // 预期大小 (用于校验)
}

// exportFileConcurrent 并发乱序下载 + WriteAt
func (e *Exporter) exportFileConcurrent(ctx context.Context, fileNode *core.FileNode, writer io.WriterAt) error {
	g, ctx := errgroup.WithContext(ctx)
	jobsCh := make(chan restoreJob, RestoreWorkerCount*2)

	// ---------------------------------------------------------
	// Stage 1: Generator (计算偏移量并分发)
	// ---------------------------------------------------------
	g.Go(func() error {
		defer close(jobsCh)
		var currentOffset int64 = 0

		// 预先计算每个 Chunk 在文件中的确切位置
		for _, chunk := range fileNode.Chunks {
			job := restoreJob{
				hash:   chunk.Cid.Hash,
				offset: currentOffset,
				size:   chunk.Size,
			}

			select {
			case jobsCh <- job:
			case <-ctx.Done():
				return ctx.Err()
			}

			// 累加偏移量
			currentOffset += int64(chunk.Size)
		}
		return nil
	})

	// ---------------------------------------------------------
	// Stage 2: Workers (下载并写入)
	// ---------------------------------------------------------
	for range RestoreWorkerCount {
		g.Go(func() error {
			for job := range jobsCh {
				// 1. 下载 Chunk
				rc, err := e.store.Get(ctx, job.hash)
				if err != nil {
					return fmt.Errorf("download chunk %s failed: %w", job.hash, err)
				}

				// 读取全部内容到内存
				// 注意：Chunk 通常很小 (8KB-64KB)，全部读入内存是安全的
				data, err := io.ReadAll(rc)
				rc.Close() // 尽早关闭连接
				if err != nil {
					return err
				}

				// 简单校验
				if len(data) != job.size {
					return fmt.Errorf("integrity error: chunk %s size mismatch (want %d, got %d)", job.hash, job.size, len(data))
				}

				// 2. 随机写入 (WriteAt)
				// 这是并发恢复的核心：只要知道 offset，谁先下载完谁就先写，不需要排队
				if _, err := writer.WriteAt(data, job.offset); err != nil {
					return fmt.Errorf("writeAt failed at offset %d: %w", job.offset, err)
				}
			}
			return nil
		})
	}

	return g.Wait()
}

func (e *Exporter) PrintObject(ctx context.Context, hash types.Hash, writer io.Writer) error {
	// 1. 读取原始字节
	reader, err := e.store.Get(ctx, hash)
	if err != nil {
		return err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	// 2. 尝试通用解码，探测类型
	// 这是一个小的性能开销，但为了 UX 是值得的
	var header struct {
		TypeVal core.ObjectType `cbor:"t"`
	}
	if err := core.DecodeObject(data, &header); err != nil {
		// 如果解不出来，说明是 Chunk (Raw Data)
		fmt.Fprintf(writer, "Type: Chunk (Raw Data)\nSize: %d bytes\n\n", len(data))
		// 对于 Chunk，为了防止终端乱码，我们只打印前 100 字节的 Hex
		// 或者你可以选择直接输出内容，视需求而定
		fmt.Fprintf(writer, "(Raw binary data not shown, use 'tv cat ... > file' to save)\n")
		return nil
	}

	// 3. 根据类型分发处理
	switch header.TypeVal {
	case core.TypeCommit:
		return printCommit(data, writer)
	case core.TypeTree:
		return printTree(data, writer)
	case core.TypeFileNode:
		// 如果是文件节点，还是走原来的“还原文件”逻辑吗？
		// 为了 cat 命令的一致性，如果是 FileNode，我们应该输出它的元数据信息
		// 如果用户想下载文件，应该用 `tv checkout` 或者 `tv cat --raw`
		// 这里我们先展示元数据
		return printFileNode(data, writer)
	default:
		return fmt.Errorf("unknown object type: %s", header.TypeVal)
	}
}

// --- 辅助打印函数 ---

func printCommit(data []byte, w io.Writer) error {
	var c core.Commit
	if err := core.DecodeObject(data, &c); err != nil {
		return err
	}
	fmt.Fprintf(w, "Type:    Commit\n")
	fmt.Fprintf(w, "Tree:    %s\n", c.TreeCid.Hash)
	for _, p := range c.Parents {
		fmt.Fprintf(w, "Parent:  %s\n", p.Hash)
	}
	fmt.Fprintf(w, "Author:  %s\n", c.Author)
	fmt.Fprintf(w, "Time:    %s\n", time.Unix(c.Timestamp, 0).Format(time.RFC3339))
	fmt.Fprintf(w, "\n%s\n", c.Message)
	return nil
}

func printTree(data []byte, w io.Writer) error {
	var t core.Tree
	if err := core.DecodeObject(data, &t); err != nil {
		return err
	}
	fmt.Fprintf(w, "Type: Tree\n\n")

	// 使用 tabwriter 对齐输出 (像 git ls-tree)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, entry := range t.Entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", entry.Type, entry.Cid.Hash[:8], entry.Name, fmtSize(entry.Size))
	}
	tw.Flush()
	return nil
}

func printFileNode(data []byte, w io.Writer) error {
	var f core.FileNode
	if err := core.DecodeObject(data, &f); err != nil {
		return err
	}
	fmt.Fprintf(w, "Type:      FileNode (ADL)\n")
	fmt.Fprintf(w, "TotalSize: %d bytes\n", f.TotalSize)
	fmt.Fprintf(w, "Chunks:    %d\n", len(f.Chunks))
	return nil
}

func fmtSize(s int64) string {
	if s == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", s)
}

type RestoreCallback func(path string, hash types.Hash, size int64)

// RestoreTree 递归地将 Merkle Tree 还原到目标目录
func (e *Exporter) RestoreTree(ctx context.Context, treeHash types.Hash, targetDir string, onRestore RestoreCallback) error {
	// 1. 获取 Tree 对象
	reader, err := e.store.Get(ctx, treeHash)
	if err != nil {
		return fmt.Errorf("failed to get tree %s: %w", treeHash, err)
	}

	treeBytes, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return err
	}

	var tree core.Tree
	if err := core.DecodeObject(treeBytes, &tree); err != nil {
		return fmt.Errorf("failed to decode tree: %w", err)
	}

	// 2. 遍历 Tree Entries
	for _, entry := range tree.Entries {
		fullPath := filepath.Join(targetDir, entry.Name)

		if entry.Type == core.EntryDir {
			// A. 处理目录：创建目录 -> 递归
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return fmt.Errorf("failed to create dir %s: %w", fullPath, err)
			}
			// 递归调用
			if err := e.RestoreTree(ctx, entry.Cid.Hash, fullPath, onRestore); err != nil {
				return err
			}
		} else {
			// B. 处理文件：导出文件 -> 触发回调
			// 创建/覆盖文件
			file, err := os.Create(fullPath)
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", fullPath, err)
			}

			// 复用已有的 ExportFile 逻辑 (流式写入)
			if err := e.ExportFile(ctx, entry.Cid.Hash, file); err != nil {
				file.Close()
				return err
			}
			file.Close()

			// 触发回调 (通知上层更新 Index)
			if onRestore != nil {
				onRestore(fullPath, entry.Cid.Hash, entry.Size)
			}
		}
	}

	return nil
}
