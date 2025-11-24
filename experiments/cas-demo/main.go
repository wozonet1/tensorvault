package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ObjectType 定义了 TensorVault 管理的对象类型
type ObjectType string

const (
	BlobObject   ObjectType = "blob"
	TreeObject   ObjectType = "tree"
	CommitObject ObjectType = "commit"
)

// TensorObject 是所有对象的通用接口
type TensorObject interface {
	Type() ObjectType
	Bytes() []byte
}

// Blob 代表一个具体的文件数据块（比如模型的一部分）
type Blob struct {
	Content []byte
}

func (b *Blob) Type() ObjectType { return BlobObject }
func (b *Blob) Bytes() []byte    { return b.Content }

// Commit 代表一个版本快照
type Commit struct {
	TreeHash   string
	ParentHash string
	Author     string
	Message    string
	Timestamp  int64
}

func (c *Commit) Type() ObjectType { return CommitObject }
func (c *Commit) Bytes() []byte {
	// 简单序列化：将 Commit 信息拼成字符串
	return fmt.Appendf(nil, "tree %s\nparent %s\nauthor %s\ntime %d\n\n%s",
		c.TreeHash, c.ParentHash, c.Author, c.Timestamp, c.Message)
}

// Store 将对象持久化到磁盘，并返回其 Hash (Content-ID)
func Store(obj TensorObject) (string, error) {
	data := obj.Bytes()

	// 1. 构造 Header (仿 Git 逻辑，但为了简单先不做 zlib 压缩)
	// 格式: "type size\0content"
	// 思考点：Git 这样做是为了类型安全，我们在 AI 场景下是否需要？
	contentWithHeader := fmt.Sprintf("%s %d\000%s", obj.Type(), len(data), data)

	// 2. 计算 SHA-256 哈希 (TensorVault 的改进点：比 SHA-1 更安全)
	hasher := sha256.New()
	hasher.Write([]byte(contentWithHeader))
	hash := hex.EncodeToString(hasher.Sum(nil))

	// 3. 确定存储路径 (Sharding)
	// 例如 hash = "a8fd..." -> 路径 = ".tensorvault/objects/a8/fd..."
	// 这种目录分片是为了避免单文件夹下文件过多导致性能下降
	dir := filepath.Join(".tensorvault", "objects", hash[:2])
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	filename := filepath.Join(dir, hash[2:])

	// 4. 写入磁盘 (原子性写入在 MVP 阶段可暂略，但博客里可以提)
	if err := os.WriteFile(filename, []byte(contentWithHeader), 0644); err != nil {
		return "", err
	}

	return hash, nil
}

func main() {
	// 模拟用户：Alice 上传了一个 AI 模型的配置文件
	modelConfig := "batch_size: 32\nlearning_rate: 0.001"

	// 1. 创建 Blob
	blob := &Blob{Content: []byte(modelConfig)}
	blobHash, _ := Store(blob)
	fmt.Printf("Blob stored. Hash: %s\n", blobHash)

	// 2. 创建 Commit (假设这是第一次提交，没有 Parent)
	commit := &Commit{
		TreeHash:   "fake_tree_hash_for_demo", // 这里为了演示简化了 Tree
		ParentHash: "",
		Author:     "Alice",
		Message:    "Initial commit for TensorVault",
		Timestamp:  time.Now().Unix(),
	}
	commitHash, _ := Store(commit)
	fmt.Printf("Commit stored. Hash: %s\n", commitHash)

	// 检查一下 .tensorvault 目录，你会发现文件已经躺在那里了
}
