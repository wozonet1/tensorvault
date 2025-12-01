package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/refs"
	"tensorvault/pkg/treebuilder"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var commitMsg string

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Record changes to the repository",
	Long:  `Create a new commit containing the current contents of the index and the given log message describing the changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 0. 防御检查
		if TV == nil {
			return fmt.Errorf("application not initialized")
		}
		if commitMsg == "" {
			return fmt.Errorf("commit message cannot be empty (use -m)")
		}

		// 1. 检查暂存区是否为空
		// Git 允许允许空提交 (git commit --allow-empty)，但 MVP 阶段我们先禁止，避免误操作
		if TV.Index.IsEmpty() {
			fmt.Println("nothing to commit, working tree clean")
			return nil
		}

		ctx := context.Background()
		start := time.Now()

		// ---------------------------------------------------------
		// Phase 1: 构建 Merkle Tree (The Heavy Lifting)
		// ---------------------------------------------------------
		fmt.Print("🔨 Building Tree... ")
		builder := treebuilder.NewBuilder(TV.Store)
		rootTreeHash, err := builder.Build(ctx, TV.Index)
		if err != nil {
			return fmt.Errorf("failed to build tree: %w", err)
		}
		fmt.Printf("Done (Root: %s)\n", rootTreeHash[:8])

		// ---------------------------------------------------------
		// Phase 2: 准备 Commit 元数据
		// ---------------------------------------------------------
		// A. 获取 Parent Commit (HEAD)
		parentHash, headVersion, err := TV.Refs.GetHead(ctx)
		var parents []string

		if err == nil {
			// 不是第一次提交，有父节点
			parents = []string{parentHash}
		} else if errors.Is(err, refs.ErrNoHead) {
			// 第一次提交 (Initial Commit)，没有父节点 -> parents 为空
			fmt.Println("🌱 Initial Commit")
		} else {
			// 真正的错误（比如文件权限问题）
			return fmt.Errorf("failed to resolve HEAD: %w", err)
		}

		// B. 获取 Author (从配置中读，如果没配就用默认值)
		author := viper.GetString("user.name")
		if author == "" {
			author = "TensorVault User"
		}

		// ---------------------------------------------------------
		// Phase 3: 创建并存储 Commit 对象
		// ---------------------------------------------------------
		commitObj, err := core.NewCommit(rootTreeHash, parents, author, commitMsg)
		if err != nil {
			return fmt.Errorf("failed to create commit object: %w", err)
		}

		// 持久化 Commit 对象
		if err := TV.Store.Put(ctx, commitObj); err != nil {
			return fmt.Errorf("failed to store commit: %w", err)
		}

		// ---------------------------------------------------------
		// Phase 4: 更新引用 (Ref Update)
		// ---------------------------------------------------------
		// 这就是“移动 HEAD 指针”
		if err := TV.Refs.UpdateHead(ctx, commitObj.ID(), headVersion); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}

		// ---------------------------------------------------------
		// Phase 5: 清理现场
		// ---------------------------------------------------------
		// 提交成功，清空暂存区
		TV.Index.Reset()
		if err := TV.Index.Save(); err != nil {
			// 这是一个尴尬的情况：Commit 成功了，但清空 Index 失败了。
			// 不应该报错导致用户以为 Commit 失败，只是打印警告。
			fmt.Printf("⚠️  Warning: failed to clear index: %v\n", err)
		}

		duration := time.Since(start)
		fmt.Printf("✅ [%s] %s\n", commitObj.ID()[:8], commitMsg)
		fmt.Printf("   Time: %s | Author: %s\n", duration, author)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(commitCmd)

	// 绑定 Flags
	commitCmd.Flags().StringVarP(&commitMsg, "message", "m", "", "commit message")
}
