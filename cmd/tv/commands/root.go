package commands

import (
	"fmt"
	"os"
	"tensorvault/pkg/app"
	"tensorvault/pkg/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	// 全局应用实例，供子命令使用
	TV *app.App
)

var rootCmd = &cobra.Command{
	Use:   "tv",
	Short: "TensorVault: AI Data Version Control",
	// 【关键】PersistentPreRunE 会在所有子命令执行前运行
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// 跳过 init 命令的依赖检查 (因为它就是去创建环境的)
		if cmd.Name() == "init" {
			return nil
		}

		// 统一初始化 App
		var err error
		TV, err = app.NewApp()
		if err != nil {
			// 友好的错误提示
			return fmt.Errorf("failed to initialize tensorvault: %w\n(Did you run 'tv init'?)", err)
		}
		return nil
	},
}

// Execute 是入口
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// 在初始化时，加载配置
	cobra.OnInitialize(initConfig)

	// 1. 定义全局参数 --config
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.tv/config.yaml)")

	// 2. 定义 storage.path 参数，并绑定到 Viper
	// 这样用户既可以在 yaml 里写，也可以用 --storage-path 覆盖
	rootCmd.PersistentFlags().String("storage-path", "", "Directory to store objects")
	err := viper.BindPFlag("storage.path", rootCmd.PersistentFlags().Lookup("storage-path"))
	if err != nil {
		fmt.Println("Failed to bind flag:", err)
		os.Exit(1)
	}
}

// initConfig 读取配置文件和环境变量
func initConfig() {
	// 直接调用共享逻辑，删掉原来那一堆代码
	if err := config.Load(cfgFile); err != nil {
		fmt.Println("Config error:", err)
		os.Exit(1)
	}
}
