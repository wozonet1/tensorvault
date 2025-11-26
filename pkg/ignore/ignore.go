package ignore

import (
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Matcher 封装了忽略逻辑
// 它负责判断一个文件是否应该被 TensorVault 忽略
type Matcher struct {
	ignorer *gitignore.GitIgnore
}

// NewMatcher 初始化忽略匹配器
// rootPath: 仓库根目录（用于查找 .tvignore 文件）
func NewMatcher(rootPath string) (*Matcher, error) {
	// 1. 定义系统级默认忽略规则 (Hardcoded Defaults)
	// 这些规则强制生效，防止用户误操作导致严重问题
	defaultRules := []string{
		// --- 关键系统目录 ---
		".tv",  // 绝对禁止索引仓库元数据目录，否则会导致无限递归死循环！
		".git", // 忽略 Git 仓库数据

		// --- 安全与配置 ---
		"config.yaml", // 防止 S3 Secret Key 泄露
		".env",        // 防止环境变量文件泄露

		// --- 常见垃圾文件 ---
		".DS_Store", // macOS
		"Thumbs.db", // Windows
	}

	var ignorer *gitignore.GitIgnore
	var err error

	// 2. 检查用户是否有 .tvignore 文件
	ignoreFilePath := filepath.Join(rootPath, ".tvignore")

	if _, errStat := os.Stat(ignoreFilePath); errStat == nil {
		// 情况 A: 用户定义了 .tvignore
		// 我们把“文件内容”和“默认规则”合并编译
		// 库函数 CompileIgnoreFileAndLines 会自动处理读取和解析
		ignorer, err = gitignore.CompileIgnoreFileAndLines(ignoreFilePath, defaultRules...)
	} else {
		// 情况 B: 用户没定义 .tvignore
		// 仅编译默认规则
		ignorer = gitignore.CompileIgnoreLines(defaultRules...)
	}

	if err != nil {
		return nil, err
	}

	return &Matcher{ignorer: ignorer}, nil
}

// Matches 检查给定的路径是否匹配忽略规则
// path: 应该是相对于仓库根目录的相对路径 (例如 "data/model.bin")
// 返回: true 表示应该忽略 (Skip), false 表示应该保留 (Keep)
// TODO: 尾部斜杠问题？
func (m *Matcher) Matches(path string) bool {
	if m.ignorer == nil {
		return false
	}
	return m.ignorer.MatchesPath(path)
}
