package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"tensorvault/pkg/exporter"
	"tensorvault/pkg/types"

	"github.com/spf13/cobra"
)

var outputFilePath string // [新增] 用于接收 -o 参数

var catCmd = &cobra.Command{
	Use:   "cat [hash-prefix]",
	Short: "Inspect an object by hash",
	Long:  `Pretty-print the contents of any object. Use -o to download binary files with high performance.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if TV == nil {
			return fmt.Errorf("app not initialized")
		}

		ctx := context.Background()
		start := time.Now()

		input := types.HashPrefix(args[0])

		// 1. 扩展 Hash
		fullHash, err := TV.Store.ExpandHash(ctx, input)
		if err != nil {
			return err
		}

		if fullHash != types.Hash(input) {
			fmt.Printf("Resolved: %s -> %s\n", input, fullHash)
		}

		exp := exporter.NewExporter(TV.Store)

		// 2. 核心分支：输出到文件还是 Stdout？
		if outputFilePath != "" {
			// [Branch A] 输出到文件 -> 触发并发恢复 (High Performance)
			f, err := os.Create(outputFilePath)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			// 即使出错也要尝试关闭文件
			defer f.Close()

			fmt.Printf("🚀 Downloading to %s (Concurrent Mode)...\n", outputFilePath)

			if err := exp.ExportFile(ctx, fullHash, f); err != nil {
				// 如果失败，最好删除半成品文件
				os.Remove(outputFilePath)
				return err
			}

			// 再次确保关闭以刷新 Buffer
			f.Close()
			fmt.Printf("✅ Done in %v\n", time.Since(start))

		} else {
			// [Branch B] 输出到终端 -> 串行流式 (Standard)
			// 注意：这里我们用 PrintObject，它内部会智能判断是打印元数据还是二进制
			if err := exp.PrintObject(ctx, fullHash, os.Stdout); err != nil {
				return err
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(catCmd)
	// [新增] 绑定 Flags
	catCmd.Flags().StringVarP(&outputFilePath, "output", "o", "", "write output to file (enables concurrent download)")
}
