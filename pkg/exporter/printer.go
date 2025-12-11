package exporter

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"tensorvault/pkg/core"
)

// PrintStructure 解析并打印结构化对象 (Commit/Tree/FileNode)
// 如果是原始数据(Chunk)，返回 false，由调用者决定如何展示
func PrintStructure(data []byte, w io.Writer) (bool, error) {
	// 1. 尝试探测类型
	var header struct {
		TypeVal core.ObjectType `cbor:"t"`
	}

	// 如果连基本的 CBOR 头都解不出来，说明是 Raw Data
	if err := core.DecodeObject(data, &header); err != nil {
		return false, nil
	}

	// 2. 分发打印
	switch header.TypeVal {
	case core.TypeCommit:
		return true, printCommit(data, w)
	case core.TypeTree:
		return true, printTree(data, w)
	case core.TypeFileNode:
		return true, printFileNode(data, w)
	default:
		// 未知类型，或者可能是巧合的二进制数据
		return false, nil
	}
}

// 下面的函数保持不变，但首字母大写以便包外复用（如果需要），或者保持小写仅供 PrintStructure 调用
// 这里保持小写，通过 PrintStructure 统一暴露

func printCommit(data []byte, w io.Writer) error {
	var c core.Commit
	if err := core.DecodeObject(data, &c); err != nil {
		return err
	}
	fmt.Fprintf(w, "Type:    Commit\n")
	fmt.Fprintf(w, "Hash:    %s\n", c.ID()) // 需确保 Commit 结构体里 Hash 被填充，或者这里只打印内容
	fmt.Fprintf(w, "Author:  %s\n", c.Author)
	fmt.Fprintf(w, "Time:    %s\n", time.Unix(c.Timestamp, 0).Format(time.RFC3339))
	fmt.Fprintf(w, "Tree:    %s\n", c.TreeCid.Hash)
	fmt.Fprintf(w, "Parents: %v\n", c.Parents)
	fmt.Fprintf(w, "\n%s\n", c.Message)
	return nil
}

func printTree(data []byte, w io.Writer) error {
	var t core.Tree
	if err := core.DecodeObject(data, &t); err != nil {
		return err
	}
	fmt.Fprintf(w, "Type: Tree\n\n")
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "MODE\tTYPE\tHASH\tSIZE\tNAME\n")
	for _, entry := range t.Entries {
		// 模拟 git ls-tree 的输出格式
		fmt.Fprintf(tw, "0644\t%s\t%s\t%s\t%s\n", entry.Type, entry.Cid.Hash[:8], fmtSize(entry.Size), entry.Name)
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
	fmt.Fprintf(w, "TotalSize: %s\n", fmtSize(f.TotalSize))
	fmt.Fprintf(w, "Chunks:    %d\n", len(f.Chunks))
	return nil
}

func fmtSize(s int64) string {
	if s < 1024 {
		return fmt.Sprintf("%dB", s)
	} else if s < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(s)/1024)
	}
	return fmt.Sprintf("%.2fMB", float64(s)/1024/1024)
}
