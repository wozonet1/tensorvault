package commands

import (
	"context"
	"fmt"
	"os"

	"tensorvault/pkg/exporter"

	"github.com/spf13/cobra"
)

var catCmd = &cobra.Command{
	Use:   "cat [hash-prefix]",
	Short: "Inspect an object by hash",
	Long:  `Pretty-print the contents of any object (Commit, Tree, FileNode) or resolve a short hash.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if TV == nil {
			return fmt.Errorf("app not initialized")
		}

		input := args[0]

		// 1. 自动扩展短哈希
		fullHash, err := TV.Store.ExpandHash(context.Background(), input)
		if err != nil {
			return err
		}

		// 如果扩展后的哈希和输入不一样，提示用户
		if fullHash != input {
			fmt.Printf("Ambiguous argument '%s': resolved to %s\n\n", input, fullHash)
		}

		// 2. 初始化 Exporter (复用 TV.Store)
		exp := exporter.NewExporter(TV.Store)

		// 3. 智能打印
		return exp.PrintObject(context.Background(), fullHash, os.Stdout)
	},
}

func init() {
	rootCmd.AddCommand(catCmd)
}
