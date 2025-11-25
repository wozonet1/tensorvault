package commands

import (
	"context"
	"fmt"
	"os"

	"tensorvault/pkg/exporter"
	"tensorvault/pkg/storage/disk"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var catCmd = &cobra.Command{
	Use:   "cat [hash]",
	Short: "Show file content by hash",
	Long:  `Retrieve the file content from the repository using its Merkle Root Hash and output to stdout.`,
	Args:  cobra.ExactArgs(1), // 必须提供 Hash
	RunE: func(cmd *cobra.Command, args []string) error {
		hash := args[0]

		storePath := viper.GetString("storage.path")
		fmt.Printf("Using storage: %s\n", storePath) // 打印一下，方便调试
		// 简单的检查：如果没 init 过，报错
		if _, err := os.Stat(storePath); os.IsNotExist(err) {
			return fmt.Errorf("not a tensorvault repository (run 'tv init' first)")
		}
		store, err := disk.NewAdapter(storePath)

		if err != nil {
		}

		// 2. 初始化 Exporter
		exp := exporter.NewExporter(store)

		// 3. 执行导出
		// 【关键点】我们将 writer 设置为 os.Stdout
		// 这样如果是文本文件，直接显示；如果是二进制，可以通过 > file.bin 重定向
		err = exp.ExportFile(context.Background(), hash, os.Stdout)
		if err != nil {
			return fmt.Errorf("cat failed: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(catCmd)
}
