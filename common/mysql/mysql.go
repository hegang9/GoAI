// Package mysql 封装 MySQL 数据库的初始化、连接池管理和基础 CRUD 操作。
//
// 使用 GORM v2 作为 ORM 框架。GORM 通过 AutoMigrate 根据 Go struct 自动创建/更新表结构，
// 避免了手动编写 DDL 的繁琐，适合快速开发迭代。
//
// 注意：包名是 mysql，而 gorm.io/driver/mysql 也导入了同名包。
// 由于本包内不直接引用 driver 包的导出符号（driver 只作为 gorm.Open 的参数），
// Go 不会产生命名冲突。这是一种常见的惯用法。
//
// 全局变量 DB 在 InitMysql() 中初始化，后续所有 dao 层操作通过 DB 进行。
package mysql

import (
	"GopherAI/config" // 项目配置，读取数据库连接参数
	"GopherAI/model"  // 数据模型，用于 AutoMigrate 和 CRUD 操作的类型参数
	"fmt"
	"time"

	"github.com/gin-gonic/gin" // Gin Web 框架，此处仅用来判断运行模式（debug/release）
	"gorm.io/driver/mysql"     // GORM 的 MySQL 驱动（注意：与本包同名但不同用途）
	"gorm.io/gorm"             // GORM ORM 核心库
	"gorm.io/gorm/logger"      // GORM 日志组件，控制 SQL 日志输出级别
)

// DB 是全局唯一的 GORM 数据库连接实例。
// 在 InitMysql() 中赋值，之后各 dao 层函数通过 mysql.DB 进行数据库操作。
// 使用包级全局变量的好处是无需显式传递，任何 import 了本包的地方都能直接使用。
var DB *gorm.DB

// InitMysql 初始化 MySQL 连接并执行自动迁移。
//
// 执行流程：
//  1. 从 TOML 配置读取连接参数
//  2. 构建 MySQL DSN（Data Source Name，数据源名称）
//  3. 根据 Gin 运行模式配置 GORM 日志级别
//  4. 调用 gorm.Open 建立连接
//  5. 配置连接池参数
//  6. 执行 AutoMigrate 自动建表
//
// 返回 error 表示初始化失败，调用方应终止程序启动。
func InitMysql() error {
	// —— 步骤 1：读取配置 ——
	host := config.GetConfig().MysqlHost           // 数据库服务器地址，如 "127.0.0.1"
	port := config.GetConfig().MysqlPort           // 端口号，默认 3306
	dbname := config.GetConfig().MysqlDatabaseName // 数据库名，如 "GopherAI"
	username := config.GetConfig().MysqlUser       // 登录用户名
	password := config.GetConfig().MysqlPassword   // 登录密码
	charset := config.GetConfig().MysqlCharset     // 字符集，推荐 utf8mb4 以支持 emoji

	// —— 步骤 2：构建 DSN（Data Source Name，数据源名称，用于告知GORM的数据库驱动“连哪里，用什么账号，什么参数） ——
	// DSN 格式: username:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=true&loc=Local
	// parseTime=true: 让 GORM 自动将 MySQL 的 DATETIME/TIMESTAMP 映射为 Go 的 time.Time
	// loc=Local: 使用服务器本地时区（部署在国内服务器的场景下，等同于 UTC+8）
	// 注释掉的那行是用 %s 格式化端口，已修正为 %d（port 是 int 类型）。
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		username, password, host, port, dbname, charset)

	// —— 步骤 3：配置日志级别 ——
	// debug 模式：打印所有 SQL 语句（开发调试用）
	// release 模式：只打印错误和慢查询（生产环境减少日志量）
	var log logger.Interface
	if gin.Mode() == "debug" {
		log = logger.Default.LogMode(logger.Info) // Info 级别会打印每条 SQL
	} else {
		log = logger.Default // 默认是 Warn 级别，仅打印慢查询
	}

	// —— 步骤 4：建立连接 ——
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       dsn,
		DefaultStringSize:         256,   // VARCHAR/STRING 字段的默认长度（不指定 size 时使用）
		DisableDatetimePrecision:  true,  // 禁用 datetime 毫秒精度（兼容旧版 MySQL）
		DontSupportRenameIndex:    true,  // 禁用重命名索引（兼容旧版 MySQL）
		DontSupportRenameColumn:   true,  // 禁用重命名列（兼容旧版 MySQL）
		SkipInitializeWithVersion: false, // 启动时检查 MySQL 版本
	}), &gorm.Config{
		Logger: log,
	})
	if err != nil {
		return err
	}

	// —— 步骤 5：配置连接池 ——
	// GORM 底层使用 database/sql 的连接池，通过 sqlDB 即可设置池化参数。
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxIdleConns(10)           // 最大空闲连接数（超过的连接会被关闭）
	sqlDB.SetMaxOpenConns(100)          // 最大打开连接数（包括使用中 + 空闲）
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最大存活时间（超时后强制回收，避免 MySQL wait_timeout 断开）

	DB = db // 赋值给全局变量，供外部使用

	// —— 步骤 6：自动迁移 ——
	return migration()
}

// migration 执行 GORM AutoMigrate，根据 model 中的 struct 定义自动同步数据库表结构。
//
// AutoMigrate 的行为：
//   - 表不存在 → 创建表
//   - 表存在但缺少列 → 添加列
//   - 表存在且列完整 → 不做任何事
//   - 不会删除已存在的列（安全策略，避免数据丢失）
//   - 不会修改列类型（需要手动迁移或使用 Migrator API）
//
// 当前迁移的模型：User（用户）、Session（会话）、Message（聊天消息）。
func migration() error {
	return DB.AutoMigrate(
		new(model.User),
		new(model.Session),
		new(model.Message),
	)
}

