// cmd/tv/commands/add.go

package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"tensorvault/pkg/ingester"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [file]",
	Short: "Add file contents to the index",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. 获取全局注入的 App
		// (架构审查：这里我们使用了 Global Variable TV，符合 Cobra 惯例，但如果追求极致洁癖，可以用 Context)
		if TV == nil {
			return fmt.Errorf("application not initialized")
		}

		filePath := args[0]

		// 2. 构造 Ingester (使用注入的 Store)
		ing := ingester.NewIngester(TV.Store)

		fmt.Printf("🚀 Ingesting %s ...\n", filePath)
		start := time.Now()

		// 3. 打开文件
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer file.Close()

		// 4. 执行切分和存储 (Heavy Lifting)
		node, err := ing.IngestFile(context.Background(), file)
		if err != nil {
			return err
		}

		// 5. 【新增】更新暂存区 (Index)
		// 注意：这里我们存的是相对路径还是绝对路径？
		// 最佳实践：存储相对于 Repo Root 的路径。MVP 简单起见，存输入路径。
		TV.Index.Add(filePath, node.ID(), node.TotalSize)

		// 6. 持久化 Index
		if err := TV.Index.Save(); err != nil {
			return fmt.Errorf("failed to update index: %w", err)
		}

		duration := time.Since(start)
		fmt.Printf("✅ Added to index in %s\n", duration)
		fmt.Printf("📦 Hash: %s\n", node.ID())

		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
