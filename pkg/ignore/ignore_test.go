package ignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatcher_Defaults(t *testing.T) {
	// 1. 创建一个空的临时目录 (模拟没有 .tvignore 的情况)
	tmpDir := t.TempDir()

	// 2. 初始化 Matcher
	matcher, err := NewMatcher(tmpDir)
	require.NoError(t, err)

	// 3. 验证默认规则
	tests := []struct {
		path     string
		shouldIg bool
	}{
		{".tv", true},
		{".tv/objects/aa", true}, // 子路径也应该被忽略
		{".git", true},
		{"config.yaml", true},
		{".DS_Store", true},
		{"main.go", false}, // 普通文件不应忽略
		{"data/model.bin", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.shouldIg, matcher.Matches(tt.path), "Path: %s", tt.path)
		})
	}
}

func TestMatcher_WithUserFile(t *testing.T) {
	// 1. 创建临时目录
	tmpDir := t.TempDir()

	// 2. 创建 .tvignore 文件，写入自定义规则
	ignoreContent := `
# 这是注释
*.log
temp
!important.log
`
	err := os.WriteFile(filepath.Join(tmpDir, ".tvignore"), []byte(ignoreContent), 0644)
	require.NoError(t, err)

	// 3. 初始化 Matcher
	matcher, err := NewMatcher(tmpDir)
	require.NoError(t, err)

	// 4. 验证混合规则 (默认 + 用户)
	tests := []struct {
		path     string
		shouldIg bool
	}{
		// --- 默认规则依然要生效 ---
		{".tv", true},
		{"config.yaml", true},

		// --- 用户规则生效 ---
		{"app.log", true},        // *.log
		{"logs/error.log", true}, // *.log 递归
		{"temp", true},           // temp/
		{"temp/file", true},

		// --- 正常文件 ---
		{"main.go", false},

		// --- 负向规则 (Whitelisting) ---
		// 注意：取决于 go-gitignore 库的具体实现，通常支持 !
		{"important.log", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.shouldIg, matcher.Matches(tt.path), "Path: %s", tt.path)
		})
	}
}
