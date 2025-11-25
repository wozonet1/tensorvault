package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm [files...]",
	Short: "Remove files from the staging area (index)",
	Long:  `Unstage files from the index. This does not delete files from the filesystem, but they will no longer be tracked in the next commit.`,
	Args:  cobra.MinimumNArgs(1), // 至少指定一个文件
	RunE: func(cmd *cobra.Command, args []string) error {
		if TV == nil {
			return fmt.Errorf("app not initialized")
		}

		// 1. 执行移除 (内存操作)
		count := 0
		for _, path := range args {
			// 为了 UX，我们可以先检查是否存在，但 delete 本身是安全的
			// 这里直接移除，追求效率
			TV.Index.Remove(path)
			fmt.Printf("Unstaged: %s\n", path)
			count++
		}

		// 2. 持久化 (原子写)
		if count > 0 {
			if err := TV.Index.Save(); err != nil {
				return fmt.Errorf("failed to save index: %w", err)
			}
			fmt.Printf("✅ Removed %d files from index.\n", count)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(rmCmd)
}
