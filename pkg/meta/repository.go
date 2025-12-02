package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"tensorvault/pkg/core"
	"tensorvault/pkg/types"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRefNotFound      = errors.New("reference not found")
	ErrConcurrentUpdate = errors.New("concurrent update detected (CAS failed)")
	ErrCommitNotFound   = errors.New("commit not found in metadata")
)

// Repository 封装所有对 SQL 数据库的操作
type Repository struct {
	db *DB
}

func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// -----------------------------------------------------------------------------
// 1. 引用管理 (Refs / Branches)
// -----------------------------------------------------------------------------

// GetRef 获取分支的当前指向 (例如 "HEAD" -> "hash...")
// 返回: CommitHash, Version, error
func (r *Repository) GetRef(ctx context.Context, name string) (*Ref, error) {
	var ref Ref
	err := r.db.GetConn().WithContext(ctx).
		Where("name = ?", name).
		First(&ref).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRefNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// UpdateRef 原子更新引用 (CAS - Compare And Swap)
// oldVersion: 你之前读到的版本号。如果数据库里现在的版本号不等于这个，说明有人抢先改了，更新失败。
func (r *Repository) UpdateRef(ctx context.Context, name string, newHash types.Hash, oldVersion int64) error {
	// 开启事务 (虽然单条 SQL 不需要显式事务，但为了扩展性保留习惯)
	return r.db.GetConn().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 场景 A: 第一次创建 (Create)
		if oldVersion == 0 {
			ref := Ref{
				Name:       name,
				CommitHash: newHash,
				Version:    1,
			}
			// 如果已经存在 (Name 冲突)，则报错
			if err := tx.Create(&ref).Error; err != nil {
				//兼容性,处理不同数据库(PG与SQLite)的唯一约束错误
				if errors.Is(err, gorm.ErrDuplicatedKey) ||
					strings.Contains(err.Error(), "UNIQUE constraint failed") {
					return ErrConcurrentUpdate
				}
				return fmt.Errorf("failed to create ref: %w", err)
			}
			return nil
		}

		// 场景 B: 更新现有引用 (Update with CAS)
		// SQL: UPDATE refs SET commit_hash = ?, version = version + 1 WHERE name = ? AND version = ?
		result := tx.Model(&Ref{}).
			Where("name = ? AND version = ?", name, oldVersion).
			Updates(map[string]any{
				"commit_hash": newHash,
				"version":     gorm.Expr("version + 1"), // 版本号自增
				"updated_at":  time.Now(),
			})

		if result.Error != nil {
			return result.Error
		}

		// 关键检查：如果影响行数为 0，说明 version 不匹配（被人抢先改了）
		if result.RowsAffected == 0 {
			return ErrConcurrentUpdate
		}

		return nil
	})
}

// -----------------------------------------------------------------------------
// 2. 提交索引 (Commit Indexing)
// -----------------------------------------------------------------------------

// IndexCommit 将 core.Commit 对象“投影”到 SQL 数据库中
// 这样我们就可以用 SQL 进行复杂查询 (按作者、时间、Meta 搜索)
func (r *Repository) IndexCommit(ctx context.Context, c *core.Commit) error {
	// 1. 转换 Parents (Link -> []string -> JSON)
	var parentHashes []types.Hash
	for _, p := range c.Parents {
		parentHashes = append(parentHashes, p.Hash)
	}
	parentsJSON, err := json.Marshal(parentHashes)
	if err != nil {
		return fmt.Errorf("failed to marshal parents: %w", err)
	}

	// 2. 构造 Model
	model := CommitModel{
		Hash:      c.ID(),
		Author:    c.Author,
		Message:   c.Message,
		Timestamp: c.Timestamp,
		TreeHash:  c.TreeCid.Hash,
		Parents:   datatypes.JSON(parentsJSON),
		CreatedAt: time.Unix(c.Timestamp, 0),
		// Meta: 未来如果有 Extra 字段，在这里 map 进去
	}

	// 3. 写入数据库 (幂等写入)
	// 如果 Hash 已存在，则什么都不做 (Do Nothing)
	err = r.db.GetConn().WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "hash"}}, // 冲突列
			DoNothing: true,                            // 忽略
		}).
		Create(&model).Error

	if err != nil {
		return fmt.Errorf("failed to index commit: %w", err)
	}
	return nil
}

func (r *Repository) GetCommit(ctx context.Context, hash types.Hash) (*CommitModel, error) {
	var commit CommitModel
	// 因为 Hash 是主键，查询非常快
	err := r.db.GetConn().WithContext(ctx).
		Where("hash = ?", hash).
		First(&commit).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCommitNotFound
	}
	if err != nil {
		return nil, err
	}
	return &commit, nil
}

// FindCommitsByAuthor 示例：利用 SQL 能力进行查询
func (r *Repository) FindCommitsByAuthor(ctx context.Context, author string, limit int) ([]CommitModel, error) {
	var commits []CommitModel
	err := r.db.GetConn().WithContext(ctx).
		Where("author = ?", author).
		Order("timestamp DESC").
		Limit(limit).
		Find(&commits).Error
	return commits, err
}
