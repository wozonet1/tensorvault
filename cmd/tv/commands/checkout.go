package commands

import (
	"context"
	"fmt"
	"io"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/exporter"
	"tensorvault/pkg/types"

	"github.com/spf13/cobra"
)

var checkoutCmd = &cobra.Command{
	Use:   "checkout [commit-hash]",
	Short: "Restore working tree files",
	Long:  `Overwrite the working tree with the content from the specified commit. This will also reset the index to match the commit.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if TV == nil {
			return fmt.Errorf("app not initialized")
		}

		ctx := context.Background()
		start := time.Now()

		// 1. 解析目标 Commit Hash
		targetInput := types.HashPrefix(args[0])
		commitHash, err := TV.Store.ExpandHash(ctx, targetInput)
		if err != nil {
			return fmt.Errorf("invalid commit '%s': %w", targetInput, err)
		}

		// 2. 获取 Commit 对象，拿到 Root Tree
		reader, err := TV.Store.Get(ctx, commitHash)
		if err != nil {
			return err
		}
		data, _ := io.ReadAll(reader)
		reader.Close()

		var commit core.Commit
		if err := core.DecodeObject(data, &commit); err != nil {
			return fmt.Errorf("failed to decode commit: %w", err)
		}

		fmt.Printf("🔄 Checking out %s (Author: %s)...\n", commitHash[:8], commit.Author)

		// 3. 准备工作区
		// MVP 策略：直接覆盖。
		// TODO: 理想情况下应该先检查是否有未提交的修改 (Dirty Check)，防止丢数据。

		// 4. 重置暂存区 (Index)
		// 我们将在还原过程中重建 Index
		TV.Index.Reset()

		// 5. 执行还原 (The Heavy Lifting)
		exp := exporter.NewExporter(TV.Store)

		// 定义回调：每还原一个文件，就往 Index 里加一条
		// 这样 Checkout 完成后，Index 的状态就和磁盘完全一致了
		restoreCallback := func(path string, hash types.Hash, size int64) {
			// 路径归一化：RestoreTree 传回来的是绝对路径或基于 CWD 的路径
			// 我们需要确保它符合 Index 的标准 (CleanPath)
			// 注意：filepath.Join 可能会产生绝对路径吗？取决于 targetDir。
			// 我们传入 "." 作为 targetDir，所以 path 是相对的。

			// 小优化：只打印大文件或每 N 个文件打印一次
			// fmt.Printf("\rRestoring: %s", path)
			TV.Index.Add(path, hash, size)
		}

		// 从当前目录 "." 开始还原
		err = exp.RestoreTree(ctx, commit.TreeCid.Hash, ".", restoreCallback)
		if err != nil {
			return fmt.Errorf("checkout failed: %w", err)
		}

		// 6. 保存 Index
		if err := TV.Index.Save(); err != nil {
			return fmt.Errorf("failed to update index: %w", err)
		}

		// 7. 更新 HEAD (Detached HEAD state)
		// (注意：这在高并发下有竞态条件，但在 CLI 场景是可接受的)
		_, currentVer, _ := TV.Refs.GetHead(ctx) // 忽略错误，如果不存在则 ver=0
		if err := TV.Refs.UpdateHead(ctx, commitHash, currentVer); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}

		fmt.Printf("\n✅ Switched to commit %s in %s\n", commitHash[:8], time.Since(start))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(checkoutCmd)
}
