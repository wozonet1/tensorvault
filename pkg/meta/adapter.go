package meta

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config 数据库配置
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string // "disable" for local
}

// DB 封装了 GORM 实例，作为元数据层的入口
type DB struct {
	conn *gorm.DB
}

// NewDB 初始化数据库连接
func NewDB(ctx context.Context, cfg Config) (*DB, error) {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=UTC",
		cfg.Host, cfg.User, cfg.Password, cfg.DBName, cfg.Port, cfg.SSLMode,
	)

	// 使用 GORM 打开连接
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// 开发阶段打开全量日志，方便看 SQL
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 获取底层 sql.DB 以配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 连接池配置 (生产环境必配)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 验证连接是否存活
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	// 自动迁移表结构
	err = db.AutoMigrate(&Ref{}, &CommitModel{}, &FileIndex{})
	if err != nil {
		return nil, fmt.Errorf("auto migration failed: %w", err)
	}

	return &DB{conn: db}, nil
}

// NewWithConnection 允许使用现有的 GORM 连接初始化 DB。
// 这对于依赖注入、复用连接池或单元测试非常有用。
func NewWithConn(conn *gorm.DB) *DB {
	return &DB{conn: conn}
}

// AutoMigrate 自动迁移表结构 (GORM 的黑魔法)
// 传入我们定义的 Model Structs
func (d *DB) AutoMigrate(models ...any) error {
	return d.conn.AutoMigrate(models...)
}

func (d *DB) GetConn() *gorm.DB {
	return d.conn
}
