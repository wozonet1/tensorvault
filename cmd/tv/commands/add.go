package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/ignore"
	"tensorvault/pkg/index"
	"tensorvault/pkg/ingester"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [path...]",
	Short: "Add file contents to the index (Smart Add)",
	Long:  `Update the index using the current content found in the working tree, to prepare the content staged for the next commit. This command handles additions, modifications, and deletions within the target path.`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if TV == nil {
			return fmt.Errorf("app not initialized")
		}

		ctx := context.Background()
		ing := ingester.NewIngester(TV.Store)
		start := time.Now()

		// 记录本次操作涉及的所有“存活”文件路径
		visited := make(map[string]bool)

		addedCount := 0
		removedCount := 0
		var totalSize int64 = 0

		wd, _ := os.Getwd()
		matcher, err := ignore.NewMatcher(wd)
		if err != nil {
			return err
		}
		// 1. 遍历并添加/更新文件 (Ingestion Phase)
		for _, targetArg := range args {
			// 清洗用户输入的路径
			targetPath := index.CleanPath(targetArg)
			absTarget, err := filepath.Abs(targetPath)
			if err != nil {
				return fmt.Errorf("failed to get absolute path for %s: %w", targetArg, err)
			}
			err = filepath.WalkDir(absTarget, func(path string, d os.DirEntry, err error) error {
				if err != nil {

					if path == absTarget {
						return err
					}
					return nil // 忽略访问受限的子文件
				}

				relPath, err := filepath.Rel(wd, path)
				if err != nil {
					return fmt.Errorf("failed to calculate relative path: %w", err)
				}

				prune := func() error {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}

				// 如果 relPath 包含 ".."，说明用户试图 add 仓库外面的文件
				if strings.HasPrefix(relPath, "..") {
					return prune()
				}

				//  检查忽略
				if matcher.Matches(relPath) {
					return prune()
				}

				if d.IsDir() {
					return nil
				}

				// 核心：Ingest
				node, err := processFile(ctx, ing, path)
				if err != nil {
					return fmt.Errorf("ingest %s failed: %w", relPath, err)
				}

				// 更新 Index
				TV.Index.Add(relPath, node.ID(), node.TotalSize)

				// 【关键】标记为“存活”
				cleanPath := index.CleanPath(relPath)
				visited[cleanPath] = true

				addedCount++
				totalSize += node.TotalSize
				fmt.Printf("\rUpdated: %s", relPath)
				return nil
			})

			// 处理 WalkDir 可能的错误（如文件完全不存在）
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		fmt.Println() // 换行

		// 2. 处理删除 (Pruning Phase)
		// 逻辑：遍历 Index 中所有已有的文件
		// 如果该文件属于我们刚才扫描的目录范围 (Target Args)，但在 visited 中不存在
		// 说明它在磁盘上已经被删除了。

		snapshot := TV.Index.Snapshot() // 获取快照，避免并发死锁
		for storedPath := range snapshot {
			// 检查 1: 这个文件是否在本次操作的范围内？
			// 如果我只 `tv add src/`，我不应该删除 `docs/readme.md`
			inScope := false
			for _, targetArg := range args {
				cleanTarget := index.CleanPath(targetArg)
				// 判断 storedPath 是否是 cleanTarget 的子路径或本身
				// 简单判断前缀 (注意要加分隔符防止 data vs database 前缀误判)
				if storedPath == cleanTarget || strings.HasPrefix(storedPath, cleanTarget+"/") {
					inScope = true
					break
				}
			}

			if !inScope {
				continue
			}

			// 检查 2: 是否被访问过？
			if !visited[storedPath] {
				// 范围在内，但没被 Walk 到 -> 说明磁盘上没了 -> 从 Index 移除
				TV.Index.Remove(storedPath)
				fmt.Printf("Deleted: %s\n", storedPath)
				removedCount++
			}
		}

		// 3. 统一落盘
		if addedCount > 0 || removedCount > 0 {
			if err := TV.Index.Save(); err != nil {
				return fmt.Errorf("save index failed: %w", err)
			}
			fmt.Printf("✅ Smart Add: %d updated, %d removed in %s\n", addedCount, removedCount, time.Since(start))
		} else {
			fmt.Println("⚠️  No changes detected.")
		}

		return nil
	},
}

// processFile 保持不变，复用之前的代码
func processFile(ctx context.Context, ing *ingester.Ingester, path string) (*core.FileNode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ing.IngestFile(ctx, f)
}

func init() {
	rootCmd.AddCommand(addCmd)
}
