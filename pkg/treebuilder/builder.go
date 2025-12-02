package treebuilder

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"tensorvault/pkg/core"
	"tensorvault/pkg/index"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/types"
)

// Builder 负责将暂存区转换为 Merkle Tree
type Builder struct {
	store storage.Store
}

func NewBuilder(store storage.Store) *Builder {
	return &Builder{store: store}
}

// Build 执行构建过程，返回根树的 Hash
func (b *Builder) Build(ctx context.Context, idx *index.Index) (types.Hash, error) {
	// 1. 构建内存中的目录树结构 (Intermediate Tree)
	root := newDirNode("")

	snapshot := idx.Snapshot()
	for path, entry := range snapshot {
		root.addFile(path, entry)
	}
	// 2. 自底向上计算 Hash 并持久化
	return b.writeNode(ctx, root)
}

// -----------------------------------------------------------------------------
// 内部辅助结构：内存树节点
// -----------------------------------------------------------------------------

type node struct {
	name     string
	isDir    bool
	children map[string]*node // 子节点 (仅目录有效)
	entry    index.Entry      // 文件元数据 (仅文件有效)
}

func newDirNode(name string) *node {
	return &node{
		name:     name,
		isDir:    true,
		children: make(map[string]*node),
	}
}

// mkdirP 递归查找或创建目录节点，返回目标目录的 node 指针
// 输入: "a/b/c" -> 返回 c 的节点
// 输入: "" 或 "." -> 返回 n (root) 自身
func (n *node) mkdirP(dirPath string) *node {
	// Base Case: 根目录
	if dirPath == "" || dirPath == "." {
		return n
	}

	parts := strings.Split(dirPath, "/")
	current := n

	for _, part := range parts {
		// 防御性编程：防止路径中有双斜杠 a//b 导致空字符串
		if part == "" {
			continue
		}

		if _, exists := current.children[part]; !exists {
			current.children[part] = newDirNode(part)
		}
		current = current.children[part]
	}

	return current
}

func (n *node) addFile(fullPath string, entry index.Entry) {
	// 1. 分离目录和文件名
	// path.Split("a/b/c.txt") -> dir="a/b/", file="c.txt"
	// path.Split("readme.md") -> dir="",       file="readme.md"
	dir, fileName := path.Split(fullPath)

	// 2. 清理路径 (去掉 path.Split 留下的尾部斜杠)
	dir = strings.TrimSuffix(dir, "/")

	// 3. 获取父节点 (语义非常清晰：去把目录建好，把爸爸给我)
	parentNode := n.mkdirP(dir)

	// 4. 挂载文件节点
	fileNode := &node{
		name:  fileName,
		isDir: false,
		entry: entry,
	}
	parentNode.children[fileName] = fileNode
}

// writeNode 递归地将内存节点转换为 core.Tree 并写入存储 (核心算法)
func (b *Builder) writeNode(ctx context.Context, n *node) (types.Hash, error) {
	// Base Case: 如果是文件，直接返回它在 Index 里记录的 FileNode Hash
	if !n.isDir {
		return n.entry.Hash, nil
	}

	// Recursive Step: 如果是目录，先递归处理所有子节点
	var entries []core.TreeEntry

	// 为了保证 Merkle Tree Hash 的确定性，必须按文件名排序处理
	// 获取所有子节点名称并排序
	childNames := make([]string, 0, len(n.children))
	for name := range n.children {
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)

	for _, name := range childNames {
		childNode := n.children[name]

		// 1. 递归获取子节点的 Hash
		childHash, err := b.writeNode(ctx, childNode)
		if err != nil {
			return "", err
		}

		var entry core.TreeEntry
		if childNode.isDir {
			// 目录：只需传名字和 Hash，Size 内部自动处理
			entry = core.NewDirEntry(name, childHash)
		} else {
			// 文件：传入名字、Hash 和从 Index 拿到的 Size
			entry = core.NewFileEntry(name, childHash, childNode.entry.Size)
		}
		entries = append(entries, entry)
	}

	// 3. 创建 core.Tree 对象
	treeObj, err := core.NewTree(entries)
	if err != nil {
		return "", fmt.Errorf("failed to create tree object: %w", err)
	}

	// 4. 持久化 Tree 对象
	if err := b.store.Put(ctx, treeObj); err != nil {
		return "", fmt.Errorf("failed to store tree: %w", err)
	}

	return treeObj.ID(), nil
}
