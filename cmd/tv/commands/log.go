package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/types"

	"github.com/spf13/cobra"
)

// TODO: 利用 refs 与meta包实现 log 命令
var logCmd = &cobra.Command{
	Use:   "log [commit-hash]",
	Short: "Show commit logs",
	Long:  `Display the commit history starting from the specified commit (or HEAD if not specified).`,
	Args:  cobra.MaximumNArgs(1), // 0 或 1 个参数
	RunE: func(cmd *cobra.Command, args []string) error {
		if TV == nil {
			return fmt.Errorf("app not initialized")
		}

		ctx := context.Background()
		var currentHash types.Hash

		// 1. 确定起始点 (Start Point)
		if len(args) > 0 {
			// 如果用户指定了 Hash (支持短哈希)

			input := types.HashPrefix(args[0])
			fullHash, err := TV.Store.ExpandHash(ctx, input)
			if err != nil {
				return fmt.Errorf("invalid commit argument '%s': %w", input, err)
			}
			currentHash = fullHash
		} else {
			// 默认从 HEAD 开始
			head, _, err := TV.Refs.GetHead(ctx)
			if errors.Is(err, refs.ErrNoHead) {
				fmt.Println("No commits yet.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to read HEAD: %w", err)
			}
			currentHash = head
		}

		// 2. 遍历链表 (Traverse the Chain)
		for currentHash != "" {
			// A. 获取 Commit 对象
			reader, err := TV.Store.Get(ctx, currentHash)
			if err != nil {
				return fmt.Errorf("failed to retrieve commit object %s: %w", currentHash, err)
			}

			data, err := io.ReadAll(reader)
			reader.Close() // 及时关闭
			if err != nil {
				return err
			}

			// B. 反序列化
			var commit core.Commit
			if err := core.DecodeObject(data, &commit); err != nil {
				return fmt.Errorf("object %s is corrupted or not a commit: %w", currentHash, err)
			}

			// C. 打印信息 (仿 Git 格式)
			printCommitLog(currentHash, &commit)

			// D. 移动指针到父节点 (Move to Parent)
			if len(commit.Parents) > 0 {
				// MVP: 默认只跟随第一个父节点 (线性历史)
				// 如果是 Merge Commit，这里忽略了其他分支，这符合 git log 的默认行为
				currentHash = commit.Parents[0].Hash
			} else {
				// 到达初始提交 (Initial Commit)，没有父节点，结束循环
				currentHash = ""
			}
		}

		return nil
	},
}

// printCommitLog 格式化输出
func printCommitLog(hash types.Hash, c *core.Commit) {
	// 颜色代码 (ANSI Escape Codes) - 可选，为了好看
	const (
		colorYellow = "\033[33m"
		colorReset  = "\033[0m"
	)

	fmt.Printf("%scommit %s%s\n", colorYellow, hash, colorReset)
	fmt.Printf("Author: %s\n", c.Author)
	fmt.Printf("Date:   %s\n", time.Unix(c.Timestamp, 0).Format(time.RFC1123))
	fmt.Printf("\n    %s\n\n", c.Message)
}

func init() {
	rootCmd.AddCommand(logCmd)
}
