package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tvrpc "tensorvault/pkg/api/tvrpc/v1"
	"tensorvault/pkg/client"

	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push [file]",
	Short: "Upload staged files (from Index) or a specific file to Server",
	Long:  `If a file argument is provided, uploads that specific file. If no argument is provided, iterates through the current Staging Area (Index) and uploads all tracked files.`,
	Args:  cobra.MaximumNArgs(1), // 0 或 1 个参数
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. 获取连接 (Lazy)
		cli, err := GetRemoteClient()
		if err != nil {
			return err
		}

		// 2. 分支逻辑
		if len(args) > 0 {
			// 模式 A: 指定文件上传 (用于调试或临时上传)
			return pushSingleFile(cmd.Context(), cli, args[0])
		}

		// 模式 B: 批量上传暂存区 (标准工作流)
		return pushStagedFiles(cmd.Context(), cli)
	},
}

// pushStagedFiles 遍历 Index 并上传
func pushStagedFiles(ctx context.Context, cli *client.TVClient) error {
	if TV.Index.IsEmpty() {
		fmt.Println("Nothing to push (index is empty). Run 'tv add <file>' first.")
		return nil
	}

	snapshot := TV.Index.Snapshot()
	fmt.Printf("📦 Pushing %d files from Staging Area...\n", len(snapshot))

	success := 0
	failures := 0

	for relPath := range snapshot {
		// 这里的 path 是相对路径，我们需要把它转为绝对路径或保持相对
		// 为了简单，假设运行命令的目录就是仓库根目录
		// 更好的做法是结合 TV.RepoPath 计算绝对路径

		fmt.Printf("Processing %s... ", relPath)

		// 检查文件是否存在于磁盘 (Index 里有但磁盘删了的情况)
		if _, err := os.Stat(relPath); os.IsNotExist(err) {
			fmt.Printf("⚠️  Skipped (Missing on disk)\n")
			failures++
			continue
		}

		// 复用单文件上传逻辑
		if err := pushSingleFile(ctx, cli, relPath); err != nil {
			fmt.Printf("❌ Failed: %v\n", err)
			failures++
		} else {
			success++
		}
	}

	fmt.Printf("\nSummary: %d succeeded, %d failed.\n", success, failures)
	if failures > 0 {
		return fmt.Errorf("some files failed to upload")
	}
	return nil
}

// pushSingleFile 封装之前的逻辑
func pushSingleFile(ctx context.Context, cli *client.TVClient, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	// 1. 计算 Linear Hash
	// 注意：这里有点性能损耗，对于大文件每次都要算一遍。
	// 未来优化：如果 Index 里存了 LinearHash，可以直接拿来用。目前先现算。
	//TODO: 未来可以考虑把 LinearHash 存到 Index 里，避免重复计算
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}
	linearHash := hex.EncodeToString(hasher.Sum(nil))

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	// 2. CheckFile
	checkResp, err := cli.Data.CheckFile(ctx, &tvrpc.CheckFileRequest{
		Sha256: linearHash,
		Size:   stat.Size(),
	})
	if err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	if checkResp.Exists {
		fmt.Printf("✅ Instant (Hash: %s...)\n", checkResp.GetMerkleRootHash()[:8])
		return nil
	}

	// 3. Upload
	stream, err := cli.Data.Upload(ctx)
	if err != nil {
		return err
	}

	err = stream.Send(&tvrpc.UploadRequest{
		Payload: &tvrpc.UploadRequest_Meta{
			Meta: &tvrpc.FileMeta{
				Path:   filepath.Base(filePath),
				Sha256: linearHash,
			},
		},
	})
	if err != nil {
		return err
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if err := stream.Send(&tvrpc.UploadRequest{
				Payload: &tvrpc.UploadRequest_ChunkData{ChunkData: buf[:n]},
			}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}

	fmt.Printf("✅ Uploaded (Hash: %s...)\n", resp.Hash[:8])
	return nil
}
func init() {
	rootCmd.AddCommand(pushCmd)
}
