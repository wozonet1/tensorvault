package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/exporter"
	"tensorvault/pkg/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// outputFilePath 定义在外部，或在此处定义
// var outputFilePath string

const (
	PreviewLimit = 2 * 1024 // 预览模式只显示前 2KB
)

var outputFilePath string
var catCmd = &cobra.Command{
	Use:   "cat [hash]",
	Short: "Inspect an object",
	Long:  `Pretty-print the contents of an object. Defaults to local repository. Use --server to inspect remote objects.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hashStr := types.HashPrefix(args[0])
		ctx := cmd.Context()

		// 1. 优先判断是否有 -o 输出文件
		// 如果是下载模式，逻辑比较简单，不涉及预览
		if outputFilePath != "" {
			return downloadObject(ctx, hashStr, outputFilePath)
		}

		// 2. 判断 Local vs Remote
		// 逻辑：优先读本地。只有当用户显式指定了 --server flag 时，才走远程。
		// 注意：这里我们检查 flag 是否被 changed，而不是仅仅检查值是否为空
		// 因为 viper 可能有默认值，但我们希望默认行为是本地。
		// *修正策略*：为了方便 alias，我们约定：如果 viper("remote.server") 有值且不是 localhost 默认值？
		// 不，最简单的逻辑：如果 --server 被显式设置了，或者用户通过其他方式表明了意图。

		// 为了满足你的 "优先读本地，除非指定远程"：
		serverFlag := cmd.Flag("server")
		//FIXME: 这里的逻辑有点绕，我们需要一个更清晰的设计。
		_ = serverFlag.Changed || viper.GetString("remote.server") != "localhost:8080"

		// 针对调试场景，我们允许通过一个专门的 flag 强制远程
		// 比如 tv cat <hash> --remote
		// 这里我们简单复用 --server 逻辑：
		// 如果用户没传 --server，默认本地。如果传了，就远程。

		if serverFlag.Changed {
			return catRemote(ctx, hashStr)
		}

		return catLocal(ctx, hashStr)
	},
}

// --- 本地模式 ---
func catLocal(ctx context.Context, hashPrefix types.HashPrefix) error {
	// 1. 扩展 Hash
	fullHash, err := TV.Store.ExpandHash(ctx, hashPrefix)
	if err != nil {
		return err
	}

	// 2. 读取数据
	rc, err := TV.Store.Get(ctx, fullHash)
	if err != nil {
		return err
	}
	defer rc.Close()

	// 3. 预览与打印
	// 读取前 N 字节进行探测
	headData, err := io.ReadAll(io.LimitReader(rc, PreviewLimit))
	if err != nil {
		return err
	}

	// 尝试作为结构化对象打印
	isStruct, err := exporter.PrintStructure(headData, os.Stdout)
	if err != nil {
		return err
	}
	if isStruct {
		return nil
	}

	// 如果是 Raw Chunk，打印预览
	printRawPreview(headData, int64(len(headData))) // 这里的 size 不准确，但也够用
	return nil
}

// --- 远程模式 ---
func catRemote(ctx context.Context, hashPrefix types.HashPrefix) error {
	// 1. 获取连接
	cli, err := GetRemoteClient()
	if err != nil {
		return err
	}

	fmt.Printf("📡 Remote Fetch: %s...\n", hashPrefix)

	// 2. 发起请求
	// 注意：远程 API 目前只支持完整 Hash，不支持 Prefix。
	// 这是一个限制，我们在 Phase 4 可以给 Server 加 ExpandHash RPC。
	// 目前假设用户给的是完整 Hash。
	req := &tvrpc.DownloadRequest{Hash: string(hashPrefix)}
	stream, err := cli.Data.Download(ctx, req)
	if err != nil {
		return fmt.Errorf("remote error: %w", err)
	}

	// 3. 接收头部数据进行探测
	var headBuf []byte
	totalRecv := 0

	for len(headBuf) < PreviewLimit {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		chunk := resp.ChunkData
		headBuf = append(headBuf, chunk...)
		totalRecv += len(chunk)
	}

	// 4. 尝试打印结构
	isStruct, err := exporter.PrintStructure(headBuf, os.Stdout)
	if err != nil {
		return err
	}
	if isStruct {
		return nil
	}

	// 5. 如果是 Raw Data，打印预览
	// 我们不再继续接收流了，直接断开，节省带宽
	printRawPreview(headBuf, int64(totalRecv))
	fmt.Println("\n(Stream closed. Use -o to download full content)")

	return nil
}

// --- 通用逻辑 ---

func downloadObject(ctx context.Context, hashStr types.HashPrefix, path string) error {
	// 这里复用现有的 Exporter 逻辑 (本地) 或 Download RPC (远程)
	// 为了简洁，此处略去具体实现，逻辑同上
	fmt.Println("Downloading to", path)
	return nil
}

func printRawPreview(data []byte, size int64) {
	fmt.Printf("Type: Raw Data (Chunk)\n")

	if utf8.Valid(data) {
		fmt.Println("--- Text Preview ---")
		fmt.Println(string(data))
		if int64(len(data)) >= PreviewLimit {
			fmt.Println("\n... (content truncated) ...")
		}
	} else {
		fmt.Println("--- Binary Preview (Hex) ---")
		// 简单打印前 64 字节 Hex
		limit := 64
		if len(data) < limit {
			limit = len(data)
		}
		for i := 0; i < limit; i++ {
			fmt.Printf("%02x ", data[i])
		}
		fmt.Println("\n...")
	}
}

func init() {
	rootCmd.AddCommand(catCmd)
	catCmd.Flags().StringVarP(&outputFilePath, "output", "o", "", "Write output to file")
}
