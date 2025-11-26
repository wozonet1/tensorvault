package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"tensorvault/pkg/app"

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
	viper.BindPFlag("storage.path", rootCmd.PersistentFlags().Lookup("storage-path"))
}

// initConfig 读取配置文件和环境变量
func initConfig() {
	if cfgFile != "" {
		// 如果用户指定了配置文件，直接用
		viper.SetConfigFile(cfgFile)
	} else {
		// 否则，按默认路径搜索配置文件
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// 默认搜索路径: ~/.tv/
		viper.AddConfigPath(".")                        // 当前目录
		viper.AddConfigPath(".tv")                      // 当前目录下的 .tv 隐藏目录 (推荐!)
		viper.AddConfigPath(filepath.Join(home, ".tv")) // 全局目录
		viper.SetConfigType("yaml")
		viper.SetConfigName("config") // 找 config.yaml
	}

	// 读取环境变量，前缀为 TV_ (例如 TV_STORAGE_PATH)
	viper.SetEnvPrefix("TV")
	viper.AutomaticEnv()

	// 设置默认值 (Default)
	// 默认存放在当前目录的 .tv/objects 下
	wd, _ := os.Getwd()
	defaultStorePath := filepath.Join(wd, ".tv", "objects")
	viper.SetDefault("storage.path", defaultStorePath)

	// 尝试读取配置
	if err := viper.ReadInConfig(); err == nil {
		// 调试用：fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}
