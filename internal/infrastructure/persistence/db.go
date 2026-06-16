package persistence

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Config 描述建立 MySQL 连接所需的参数，由 bootstrap 从应用配置填充。
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	Charset  string
	// Debug 为真时打印全部 SQL（开发环境），否则仅打印慢查询。
	Debug bool
}

// Connect 建立 MySQL 连接、配置连接池并执行自动迁移，返回 *gorm.DB。
//
// 注意：迁移逻辑集中在持久化层（而非由基础设施反向依赖领域 model），
// 迁移的目标是本层定义的 PO。
func Connect(cfg Config) (*gorm.DB, error) {
	// DSN: user:password@tcp(host:port)/dbname?charset=...&parseTime=true&loc=Local
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.Charset)

	var log gormlogger.Interface
	if cfg.Debug {
		log = gormlogger.Default.LogMode(gormlogger.Info)
	} else {
		log = gormlogger.Default
	}

	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       dsn,
		DefaultStringSize:         256,
		DisableDatetimePrecision:  true,
		DontSupportRenameIndex:    true,
		DontSupportRenameColumn:   true,
		SkipInitializeWithVersion: false,
	}), &gorm.Config{Logger: log})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

// Close 关闭底层数据库连接池，供优雅退出时释放资源。
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// migrate 执行 GORM AutoMigrate，根据 PO 定义同步数据库表结构。
func migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		new(UserPO),
		new(SessionPO),
		new(MessagePO),
	)
}
