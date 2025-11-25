package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a TensorVault repository",
	Long:  `Create an empty TensorVault repository or reinitialize an existing one.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. 获取当前路径
		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		// 2. 定义仓库路径 (.tv)
		repoPath := filepath.Join(wd, ".tv")
		objectsPath := filepath.Join(repoPath, "objects")

		// 3. 检查是否已存在
		if _, err := os.Stat(repoPath); err == nil {
			fmt.Printf("⚠️  TensorVault repository already exists in %s\n", repoPath)
			return nil
		}

		// 4. 创建目录结构
		// 我们需要 .tv/objects 来存放 chunks
		if err := os.MkdirAll(objectsPath, 0755); err != nil {
			return fmt.Errorf("failed to create repo directory: %w", err)
		}

		fmt.Printf("✅ Initialized empty TensorVault repository in %s\n", repoPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
