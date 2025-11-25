package treebuilder

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"tensorvault/pkg/core"
	"tensorvault/pkg/index"
	"tensorvault/pkg/storage"
)

// Builder 负责将暂存区转换为 Merkle Tree
type Builder struct {
	store storage.Store
}

func NewBuilder(store storage.Store) *Builder {
	return &Builder{store: store}
}

// Build 执行构建过程，返回根树的 Hash
func (b *Builder) Build(ctx context.Context, idx *index.Index) (string, error) {
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

// addFile 将一个文件路径插入到内存树中
// 例如 path="a/b/c.txt" -> 递归创建 a, b, 然后在 b 下创建 c.txt
func (n *node) addFile(path string, entry index.Entry) {
	parts := strings.Split(path, "/")
	current := n

	// 遍历路径中的目录部分
	for _, part := range parts[:len(parts)-1] {
		if _, exists := current.children[part]; !exists {
			current.children[part] = newDirNode(part)
		}
		current = current.children[part]
	}

	// 创建文件节点
	fileName := parts[len(parts)-1]
	fileNode := &node{
		name:  fileName,
		isDir: false,
		entry: entry, // 保存 Index 中的元数据 (Hash, Size)
	}
	current.children[fileName] = fileNode
}

// writeNode 递归地将内存节点转换为 core.Tree 并写入存储 (核心算法)
func (b *Builder) writeNode(ctx context.Context, n *node) (string, error) {
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

		// 2. 构造 TreeEntry
		mode := core.EntryFile
		if childNode.isDir {
			mode = core.EntryDir
		}

		// 注意：如果是目录，Size 怎么算？通常 Git 目录 Size 为 0，或者累加。
		// 这里简单起见，目录 Size 设为 0，文件 Size 从 Index 获取
		size := int64(0)
		if !childNode.isDir {
			size = childNode.entry.Size
		}

		entries = append(entries, core.TreeEntry{
			Name: name,
			Type: mode,
			Cid:  core.NewLink(childHash), // 使用重构后的 Cid
			Size: size,
		})
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
