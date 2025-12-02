package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"tensorvault/pkg/core"
	"tensorvault/pkg/storage"
	"tensorvault/pkg/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Adapter 实现了 storage.Store 接口
type Adapter struct {
	client *s3.Client
	bucket string
}

// Config 用于初始化 Adapter
type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

// NewAdapter 初始化 S3 客户端 (适配 AWS SDK v2 最新规范)
func NewAdapter(ctx context.Context, cfg Config) (*Adapter, error) {
	// 1. 加载基础配置 (仅包含 Region 和 Credentials)
	// 不再这里配置 EndpointResolver
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %w", err)
	}

	// 2. 创建 S3 客户端时，注入特定于 S3 的配置
	// 这是新版 SDK 推荐的做法：使用 BaseEndpoint 而不是全局 Resolver
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// 如果指定了 Endpoint (比如 MinIO 的 localhost:9000)，则覆盖默认值
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}

		// 【关键】MinIO 必须强制使用 Path Style
		// 即: http://host:9000/bucket/key
		// 而不是: http://bucket.host:9000/key (Virtual Hosted Style)
		o.UsePathStyle = true
	})

	// 3. (可选) 自动创建 Bucket 逻辑保持不变
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &cfg.Bucket})
	if err != nil {
		// 如果 Head 失败，尝试创建
		_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &cfg.Bucket})
		if err != nil {
			// 这里可能因为并发创建或权限问题报错，但在 MVP 中我们先继续
			// 生产环境建议手动管理 Bucket
			fmt.Printf("Warning: failed to ensure bucket exists: %v\n", err)
		}
	}

	return &Adapter{
		client: client,
		bucket: cfg.Bucket,
	}, nil
}

// transformKey 将 Hash 转换为 S3 Key (Sharding)
// Logic: "aabbcc..." -> "aa/bbcc..."
func (s *Adapter) transformKey(hash types.Hash) string {
	hashStr := string(hash)
	if len(hashStr) < 2 {
		return hashStr
	}
	return hashStr[:2] + "/" + hashStr[2:]
}

// Put 上传对象
func (s *Adapter) Put(ctx context.Context, obj core.Object) error {
	// 1. 幂等性检查 (去重)
	// 对于 S3，Head 请求比 Put 请求便宜且快。如果已存在，直接跳过。
	exists, err := s.Has(ctx, obj.ID())
	if err != nil {
		return fmt.Errorf("s3 put existence check failed: %w", err)
	}
	if exists {
		return nil
	}

	key := s.transformKey(obj.ID())
	data := obj.Bytes()

	// 2. 执行上传
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
		// 标记 Content-Type 有助于在浏览器中预览，虽然对逻辑无影响
		ContentType: aws.String("application/cbor"),
	})

	if err != nil {
		return fmt.Errorf("s3 put failed: %w", err)
	}
	return nil
}

// Get 下载对象
func (s *Adapter) Get(ctx context.Context, hash types.Hash) (io.ReadCloser, error) {
	key := s.transformKey(hash)

	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		// 将 AWS 的 NoSuchKey 错误映射为我们自己的 ErrNotFound
		var noKey *s3types.NoSuchKey
		if errors.As(err, &noKey) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("s3 get failed: %w", err)
	}

	return resp.Body, nil
}

// Has 检查对象是否存在
func (s *Adapter) Has(ctx context.Context, hash types.Hash) (bool, error) {
	key := s.transformKey(hash)

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err == nil {
		return true, nil
	}

	var notFound *s3types.NotFound
	var noKey *s3types.NoSuchKey
	if errors.As(err, &notFound) || errors.As(err, &noKey) {
		return false, nil
	}
	// 兼容性：某些 S3 实现可能返回 generic 404 error string
	if strings.Contains(err.Error(), "404") {
		return false, nil
	}

	return false, err
}

// ExpandHash 利用 Prefix 查询扩展短哈希
func (s *Adapter) ExpandHash(ctx context.Context, shortHash types.HashPrefix) (types.Hash, error) {
	inputStr := string(shortHash)
	if len(inputStr) < 4 {
		return "", fmt.Errorf("hash prefix too short")
	}

	// 构造前缀: "a8fd" -> "a8/fd"
	prefix := inputStr[:2] + "/" + inputStr[2:]

	// 这里的 MaxKeys=2 是关键：我们只需要知道是否有 0 个、1 个(唯一) 或 >1 个(歧义)
	resp, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(2),
	})

	if err != nil {
		return "", fmt.Errorf("s3 list failed: %w", err)
	}

	if *resp.KeyCount == 0 {
		return "", storage.ErrNotFound
	}

	if *resp.KeyCount > 1 {
		return "", storage.ErrAmbiguousHash
	}

	// 还原 Hash: 拿到 Key "a8/fd123..." -> 去掉中间的 "/" -> "a8fd123..."
	key := *resp.Contents[0].Key
	hash := strings.Replace(key, "/", "", 1)

	return types.Hash(hash), nil
}
