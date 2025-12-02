package app

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitStore_Disk(t *testing.T) {
	// 1. Mock 配置
	viper.Reset()
	viper.Set("storage.type", "disk")
	viper.Set("storage.path", "/tmp/tv-test/objects")

	// 2. 调用私有函数 (因为我们在同一个包)
	store, err := initStore(context.Background(), "/tmp/tv-test")

	// 3. 验证
	require.NoError(t, err)
	assert.NotNil(t, store)
	// 可以断言 store 的类型是 *disk.Adapter
}

func TestInitStore_S3_MissingBucket(t *testing.T) {
	viper.Reset()
	viper.Set("storage.type", "s3")
	// 故意不设置 bucket

	store, err := initStore(context.Background(), ".")
	assert.Error(t, err)
	assert.Nil(t, store)
	assert.Contains(t, err.Error(), "bucket is required")
}

func TestInitStore_UnknownType(t *testing.T) {
	viper.Reset()
	viper.Set("storage.type", "ftp") // 不支持的类型

	store, err := initStore(context.Background(), ".")
	assert.Error(t, err)
	assert.Nil(t, store)
	assert.Contains(t, err.Error(), "unsupported storage type")
}
