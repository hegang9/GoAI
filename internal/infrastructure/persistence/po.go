// Package persistence 是持久化适配层：封装 GORM 连接与迁移，
// 并以「持久化对象（PO）+ 仓储实现」的方式落地领域层定义的仓储端口。
//
// PO（Persistence Object）带有 gorm/json 标签，仅服务于数据库映射；
// 仓储实现负责在 PO 与领域实体之间转换，从而让领域层不感知数据库细节。
package persistence

import (
	"time"

	"gorm.io/gorm"
)

// UserPO 用户持久化对象，对应数据库 users 表。
type UserPO struct {
	// ID 主键。
	ID int64 `gorm:"primaryKey" json:"id"`
	// Name 昵称/显示名，允许重复。
	Name string `gorm:"type:varchar(50)" json:"name"`
	// Email 邮箱，唯一索引。
	Email string `gorm:"type:varchar(100);uniqueIndex;not null" json:"email"`
	// AccountNo 内部账号编号，唯一索引。
	AccountNo string `gorm:"type:varchar(50);uniqueIndex;not null" json:"account_no"`
	// Password 密码哈希，序列化时跳过，避免泄露。
	Password string `gorm:"type:varchar(255)" json:"-"`
	// CreatedAt 创建时间，GORM 自动填充。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间，GORM 自动填充。
	UpdatedAt time.Time `json:"updated_at"`
	// DeletedAt 软删除标记。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 显式指定用户表名。
func (UserPO) TableName() string { return "users" }

// SessionPO 会话持久化对象，对应数据库 sessions 表。
type SessionPO struct {
	// ID 会话唯一标识（UUID）。
	ID string `gorm:"primaryKey;type:varchar(36)" json:"id"`
	// AccountNo 会话归属的内部账号编号。
	AccountNo string `gorm:"index;not null;column:account_no" json:"account_no"`
	// Title 会话标题。
	Title string `gorm:"type:varchar(100)" json:"title"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `json:"updated_at"`
	// DeletedAt 软删除标记。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 显式指定会话表名。
func (SessionPO) TableName() string { return "sessions" }

// MessagePO 消息持久化对象，对应数据库 messages 表。
type MessagePO struct {
	// ID 自增主键。
	ID uint `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	// SessionID 所属会话标识，建立索引以加速按会话查询。
	SessionID string `gorm:"index;not null;type:varchar(36);column:session_id" json:"session_id"`
	// AccountNo 消息归属的内部账号编号。
	AccountNo string `gorm:"type:varchar(50);column:account_no" json:"account_no"`
	// Content 消息正文。
	Content string `gorm:"type:text;column:content" json:"content"`
	// IsUser 标识来源：true 用户、false AI。
	IsUser bool `gorm:"not null;column:is_user" json:"is_user"`
	// CreatedAt 创建时间，GORM 自动填充。
	CreatedAt time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
}

// TableName 显式指定消息表名。
func (MessagePO) TableName() string { return "messages" }
