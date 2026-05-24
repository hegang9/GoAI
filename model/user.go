// Package model 定义应用的数据模型（数据库表结构的 Go 语言映射）。
//
// 什么是 GORM？
// =============
// GORM（Go Object Relational Mapping）是 Go 语言中最流行的 ORM 框架。
// ORM 的作用是在面向对象的 Go struct 和关系型数据库表之间建立映射，
// 让你可以用 Go 代码操作数据库，而不必手写 SQL。
//
// GORM 的核心功能：
//   - 通过 struct tag（如 `gorm:"primaryKey"`）声明字段约束
//   - AutoMigrate：根据 struct 定义自动创建/更新数据库表结构
//   - CRUD 操作：Create、Find、Update、Delete 方法链式调用
//   - 软删除：标记删除时间而不真正删除数据行
//   - 关联关系：HasOne、HasMany、BelongsTo、ManyToMany
//   - 钩子函数：BeforeCreate、AfterUpdate 等生命周期回调
//   - 事务、预编译（防 SQL 注入）、迁移、日志等
//
// struct tag 格式说明：
//
//	gorm:"..."   → 告诉 GORM 如何映射该字段到数据库列
//	json:"..."   → 告诉 Go JSON 序列化器如何处理该字段
//	两者互不干扰，可以同时使用
package model

import (
	"time" // time.Time: 时间戳类型

	"gorm.io/gorm" // GORM v2: gorm.DeletedAt 用于软删除支持
)

// User 代表系统用户，对应数据库中的 users 表（GORM 会自动将 struct 名转为蛇形命名作为表名）。
type User struct {
	// ID 是主键，GORM 默认将名为 ID 的字段作为主键，这里显式声明 primaryKey。
	// 类型为 int64 适合自增 ID，能容纳非常大的数据量（约 9.2 × 10¹⁸）。
	ID int64 `gorm:"primaryKey" json:"id"`

	// Name 存储用户昵称/显示名，varchar(50) 限制最多 50 个字符，避免过长输入。
	Name string `gorm:"type:varchar(50)" json:"name"`

	// Email 存储邮箱地址，varchar(100) 足够容纳常见邮箱。
	// index 标签会为该列创建普通索引，加速按邮箱查询（如登录、查找用户）。
	Email string `gorm:"type:varchar(100);index" json:"email"`

	// Username 是登录用户名，uniqueIndex 创建唯一索引，保证不重复。
	// 数据库层面强制约束，比应用层校验更可靠。
	Username string `gorm:"type:varchar(50);uniqueIndex" json:"username"`

	// Password 存储密码哈希（如 bcrypt 结果），255 长度足够容纳各种哈希算法。
	// json:"-" 表示序列化为 JSON 时跳过该字段，绝不让密码哈希泄露到前端响应中。
	Password string `gorm:"type:varchar(255)" json:"-"`

	// CreatedAt 记录创建时间，GORM 在 Create 时自动设置为当前时间。
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 记录最后更新时间，GORM 在 Create/Update 时自动设置为当前时间。
	UpdatedAt time.Time `json:"updated_at"`

	// DeletedAt 支持软删除：调用 Delete 时不真正删除行，而是填充此字段。
	// 为 nil 表示未删除，非 nil 表示已删除。GORM 自动在查询时过滤已删除记录。
	// index 加速按删除状态筛选，json:"-" 防止暴露删除状态给前端。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
