package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// 默认配置模板
const defaultConfigTemplate = `# TensorVault Configuration

# [Client & Server] Storage Backend
storage:
  type: "s3"
  s3:
    endpoint: "http://localhost:9000"
    region: "us-east-1"
    bucket: "tensorvault-dev"
    access_key_id: "admin"
    secret_access_key: "password"
  cache:
    enabled: true
    redis_url: "redis://localhost:6379/0"
    ttl: "24h"

# [Client] Remote Server Address
remote:
  server: "localhost:8080"

# [Server Only] Database Configuration
# CLI users can ignore this section
database:
  host: "localhost"
  port: 5432
  user: "tv_user"
  password: "tv_password"
  dbname: "tensorvault"
  sslmode: "disable"

# User Identity
user:
  name: "Anonymous"
  email: "anon@tensorvault.io"
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a TensorVault repository",
	Long:  `Create an empty TensorVault repository and default configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		// 1. 创建目录结构
		repoPath := filepath.Join(wd, ".tv")
		objectsPath := filepath.Join(repoPath, "objects")
		if err := os.MkdirAll(objectsPath, 0755); err != nil {
			return fmt.Errorf("failed to create repo directory: %w", err)
		}

		fmt.Printf("✅ Initialized empty TensorVault repository in %s\n", repoPath)

		// 2. [新增] 生成配置文件
		configPath := filepath.Join(repoPath, "config.yaml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if err := os.WriteFile(configPath, []byte(defaultConfigTemplate), 0644); err != nil {
				return fmt.Errorf("failed to create config file: %w", err)
			}
			fmt.Printf("📝 Generated default configuration at %s\n", configPath)
		} else {
			fmt.Printf("ℹ️  Config file already exists at %s\n", configPath)
		}

		// 3. [新增] 初始化空的 index.json (防止首次 add 报错)
		indexPath := filepath.Join(repoPath, "index.json")
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			if err := os.WriteFile(indexPath, []byte("{}"), 0644); err != nil {
				return fmt.Errorf("failed to init index: %w", err)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
