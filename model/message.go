// Package model 定义数据模型（GORM 映射到数据库表）。
package model

import (
	"time"
)

// Message 聊天消息模型，对应数据库中的 messages 表。
// 每一条记录代表会话中的一次对话（用户提问或 AI 回复）。
type Message struct {
	// ID 主键，自增唯一标识每条消息。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	// SessionID 所属会话的唯一标识，通过外键关联到 sessions 表。
	// 建立索引以加速按会话查询消息的效率。
	SessionID string `gorm:"index;not null;type:varchar(36)" json:"session_id"`

	// UserName 发送消息的用户名，用于标识消息归属。
	UserName string `gorm:"type:varchar(20)" json:"username"`

	// Content 消息正文，使用 text 类型支持长文本（如 AI 生成的代码或长回复）。
	Content string `gorm:"type:text" json:"content"`

	// IsUser 标识消息来源：true 表示用户发送，false 表示 AI 回复。
	// 前端凭此区分聊天气泡的展示样式（左右对齐）。
	IsUser bool `gorm:"not null;" json:"is_user"`

	// CreatedAt 消息创建时间，由 GORM 在插入记录时自动填充。
	CreatedAt time.Time `json:"created_at"`
}
