// cmd/tv/commands/add.go

package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/ingester"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [file]",
	Short: "Add file contents to the index",
	Args:  cobra.ExactArgs(1),
	// ... imports 增加 "path/filepath", "strings"

	RunE: func(cmd *cobra.Command, args []string) error {
		if TV == nil {
			return fmt.Errorf("app not initialized")
		}
		targetPath := args[0] // 用户输入的路径，可能是文件，也可能是目录

		// 1. 准备工作
		ctx := context.Background()
		ing := ingester.NewIngester(TV.Store)
		start := time.Now()

		addedCount := 0
		var totalSize int64 = 0

		// 2. 定义遍历函数 (Walker)
		// 这是一个闭包，它会处理每一个找到的文件
		walkFn := func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err // 权限错误等
			}

			// [安全防御]：永远跳过 .tv 目录
			if d.IsDir() && d.Name() == ".tv" {
				return filepath.SkipDir
			}

			// [安全防御]：跳过 .git 等隐藏目录 (可选，但建议加上)
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != "." && d.Name() != ".." {
				// return filepath.SkipDir // 如果你想默认忽略隐藏文件夹，取消注释
			}

			// 我们只处理文件，目录本身不需要 "add"，因为 TreeBuilder 会根据文件路径自动重建目录
			if d.IsDir() {
				return nil
			}

			node, err := processFile(ctx, ing, path)

			if err != nil {
				return fmt.Errorf("failed to ingest %s: %w", path, err)
			}

			// 更新暂存区 (内存操作，很快)
			// 注意：path 是相对于运行目录的。最好将其转换为相对于 Repo Root 的路径。
			// MVP 阶段假设用户就在 Root 运行，直接用 path。
			TV.Index.Add(path, node.ID(), node.TotalSize)

			addedCount++
			totalSize += node.TotalSize
			fmt.Printf("\rAdding: %s (%d)", path, node.TotalSize) // \r 简单进度条
			return nil
		}

		// 3. 执行遍历
		// 如果 targetPath 是文件，WalkDir 也会正常工作（只回调一次）
		if err := filepath.WalkDir(targetPath, walkFn); err != nil {
			return fmt.Errorf("walk failed: %w", err)
		}
		fmt.Println() // 换行

		// 4. 批量落盘 (Batch Commit to Index)
		if addedCount > 0 {
			if err := TV.Index.Save(); err != nil {
				return fmt.Errorf("failed to save index: %w", err)
			}
			duration := time.Since(start)
			fmt.Printf("✅ Added %d files (%d) in %s\n", addedCount, totalSize, duration)
		} else {
			fmt.Println("⚠️  No files added.")
		}

		return nil
	},
}

func processFile(ctx context.Context, ing *ingester.Ingester, path string) (*core.FileNode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() // 这里用 defer 是 100% 安全的，函数返回即关闭
	return ing.IngestFile(ctx, f)
}
func init() {
	rootCmd.AddCommand(addCmd)
}
