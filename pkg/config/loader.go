package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Load 初始化 Viper 配置
// cfgFile: 可选，用户显式指定的配置文件路径
func Load(cfgFile string) error {
	// 1. 设置默认值 (Defaults)
	setDefaults()

	// 2. 配置搜索路径
	if cfgFile != "" {
		// 如果用户指定了文件，直接使用
		viper.SetConfigFile(cfgFile)
	} else {
		// 否则按优先级搜索
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		// 搜索顺序：
		// 1. 当前目录
		viper.AddConfigPath(".")
		// 2. 当前目录下的 .tv
		viper.AddConfigPath(".tv")
		// 3. 用户主目录下的 .tv
		viper.AddConfigPath(filepath.Join(home, ".tv"))

		viper.SetConfigType("yaml")
		viper.SetConfigName("config") // 找 config.yaml
	}

	// 3. 读取环境变量 (TV_DATABASE_HOST 等)
	viper.SetEnvPrefix("TV")
	viper.AutomaticEnv()

	// 4. 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		// 如果只是没找到配置文件，但可能有环境变量，不一定算错
		// 但如果是配置文件格式错，那就是错
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found; ignore error if desired
			fmt.Println("⚠️  No config file found, using defaults/env vars")
		} else {
			// Config file was found but another error produced
			return fmt.Errorf("fatal error config file: %w", err)
		}
	} else {
		fmt.Println("🔧 Using config file:", viper.ConfigFileUsed())
	}

	return nil
}

func setDefaults() {
	// 数据库默认值
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.sslmode", "disable")

	// 存储默认值
	wd, _ := os.Getwd()
	defaultStorePath := filepath.Join(wd, ".tv", "objects")
	viper.SetDefault("storage.path", defaultStorePath)
	viper.SetDefault("storage.type", "disk")
}
