// Package chat 是会话领域：承载会话/消息实体、会话聚合（Conversation）、
// 会话管理器（Manager）以及该领域依赖的端口接口（模型、模型工厂、消息持久化）。
//
// 该包不依赖任何外层（gin / gorm / eino / rabbitmq / config 等）：
//   - 与具体模型实现的协作通过 Model / ModelFactory 端口完成；
//   - 与持久化的协作通过 MessageSink / MessageRepository / SessionRepository 端口完成。
package chat

// MessageCreatedTopic 是聊天消息创建事件的稳定 Topic。
const MessageCreatedTopic = "chat.message.created.v1"

// Message 是会话中的一条消息领域值对象，独立于数据库模型。
type Message struct {
	// 全局唯一标识，支撑幂等
	ID string
	// SessionID 所属会话标识。
	SessionID string
	// AccountNo 消息归属的内部账号编号。
	AccountNo string
	// Content 消息正文。
	Content string
	// IsUser 标识来源：true 为用户消息，false 为 AI 回复。
	IsUser bool
}

// Session 是一次对话会话的领域值对象。
type Session struct {
	// ID 会话唯一标识（UUID）。
	ID string
	// AccountNo 会话归属的内部账号编号。
	AccountNo string
	// Title 会话标题，默认取首条用户问题。
	Title string
}
