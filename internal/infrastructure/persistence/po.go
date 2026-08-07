// Package persistence 是持久化适配层：封装 GORM 连接与迁移，
// 并以「持久化对象（PO）+ 仓储实现」的方式落地领域层定义的仓储端口。
// PO（Persistence Object）仅带 gorm 标签服务于数据库映射；HTTP JSON 契约由 interfaces/http/dto 承担。
// 仓储实现负责在 PO 与领域实体之间转换，从而让领域层不感知数据库细节。
package persistence

import (
	"time"

	"gorm.io/gorm"
)

// UserPO 用户持久化对象，对应数据库 users 表。
type UserPO struct {

	// ID 主键。
	ID int64 `gorm:"primaryKey;column:id"`

	// Name 昵称/显示名，允许重复。
	Name string `gorm:"type:varchar(50);column:name"`

	// Email 邮箱，唯一索引。
	Email string `gorm:"type:varchar(100);uniqueIndex;not null;column:email"`

	// AccountNo 内部账号编号，唯一索引。
	AccountNo string `gorm:"type:varchar(50);uniqueIndex;not null;column:account_no"`

	// Password 密码哈希；json:"-" 防止调试序列化时意外泄露。
	Password string `gorm:"type:varchar(255);column:pass_word" json:"-"`

	// CreatedAt 创建时间，GORM 按字段名约定自动填充。
	CreatedAt time.Time

	// UpdatedAt 更新时间，GORM 按字段名约定自动填充。
	UpdatedAt time.Time

	// DeletedAt 软删除标记。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 显式指定用户表名。
func (UserPO) TableName() string { return "users" }

// SessionPO 会话持久化对象，对应数据库 sessions 表。
type SessionPO struct {

	// ID 会话唯一标识（UUID）。
	ID string `gorm:"primaryKey;type:varchar(36);column:id"`

	// AccountNo 会话归属的内部账号编号。
	AccountNo string `gorm:"index;not null;column:account_no;column:account_no"`

	// Title 会话标题。
	Title string `gorm:"type:varchar(100);column:title"`

	// CreatedAt 创建时间，GORM 按字段名约定自动填充。
	CreatedAt time.Time

	// UpdatedAt 更新时间，GORM 按字段名约定自动填充。
	UpdatedAt time.Time

	// DeletedAt 软删除标记。
	DeletedAt gorm.DeletedAt `gorm:"index;" json:"-"`
}

// TableName 显式指定会话表名。
func (SessionPO) TableName() string { return "sessions" }

// MessagePO 消息持久化对象，对应数据库 messages 表。
type MessagePO struct {

	// ID 自增主键。
	ID uint `gorm:"primaryKey;autoIncrement;column:id;index:idx_message_replay"`

	// MessageID 是跨 RabbitMQ 投递保持不变的消息唯一标识
	MessageID string `gorm:"type:varchar(36);not null;column:message_id;uniqueIndex:uk_messages_message_id"`

	// SessionID 所属会话标识，建立索引以加速按会话查询。
	SessionID string `gorm:"index;not null;type:varchar(36);column:session_id;index:idx_message_replay"`

	// AccountNo 消息归属的内部账号编号。
	AccountNo string `gorm:"type:varchar(50);column:account_no"`

	// Content 消息正文。
	Content string `gorm:"type:text;column:content"`

	// IsUser 标识来源：true 用户、false AI。
	IsUser bool `gorm:"not null;column:is_user"`

	// CreatedAt 创建时间，GORM 自动填充。
	CreatedAt time.Time `gorm:"autoCreateTime;column:created_at;index:idx_message_replay"`
}

// TableName 显式指定消息表名。
func (MessagePO) TableName() string { return "messages" }

// DelayTaskPO 延迟任务持久化对象，对应数据库 delay_tasks 表。
type DelayTaskPO struct {
	ID          string    `gorm:"primaryKey;type:varchar(36);column:id"`
	AccountNo   string    `gorm:"index;not null;column:account_no"`
	Destination string    `gorm:"type:varchar(255);column:destination"`
	TargetAt    int64     `gorm:"not null;column:target_at"`
	Payload     []byte    `gorm:"type:text;column:payload"`
	Version     int64     `gorm:"not null;column:version"`
	Status      uint8     `gorm:"not null;column:status"`
	TaskHash    []byte    `gorm:"type:binary(32);not null;column:task_hash"`
	CreatedAt   time.Time `gorm:"autoCreateTime;column:created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;column:updated_at"`
}

func (DelayTaskPO) TableName() string { return "delay_tasks" }
