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
)

type Exporter struct {
	store storage.Store
}

func NewExporter(store storage.Store) *Exporter {
	return &Exporter{store: store}
}

// ExportFile 根据 FileNode 的 Hash，将还原的文件写入 writer
func (e *Exporter) ExportFile(ctx context.Context, hash types.Hash, writer io.Writer) error {
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
