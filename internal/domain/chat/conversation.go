package chat

import (
	"context"
	"sync"

	"GopherAI/pkg/id"
)

// Conversation 是“单个会话”的领域聚合：持有会话消息历史、绑定的模型，
// 并通过 MessageSink 端口触发消息的异步持久化。
//
// 它取代了旧的 common/aihelper/session.AIHelper，但不再直接依赖 RabbitMQ、
// 数据库模型或消息转换实现——这些都通过端口与领域类型完成。
type Conversation struct {
	// sessionID 当前会话标识。
	sessionID string
	// model 当前会话使用的模型实现（端口）。
	model Model
	// sink 消息持久化端口（异步落库）。
	sink MessageSink
	// messages 会话消息历史。
	messages []Message
	// mu 保护消息历史的并发读写。
	mu sync.RWMutex
}

// NewConversation 创建会话聚合实例。
func NewConversation(model Model, sessionID string, sink MessageSink) *Conversation {
	return &Conversation{
		sessionID: sessionID,
		model:     model,
		sink:      sink,
		messages:  make([]Message, 0),
	}
}

// SessionID 返回会话标识。
func (c *Conversation) SessionID() string { return c.sessionID }

// ModelType 返回当前会话使用的模型类型。
func (c *Conversation) ModelType() string { return c.model.Type() }

// AddMessage 追加一条消息到内存历史，并在 persist 为真时通过 Sink 异步持久化。
//
// 回放历史时传入 persist=false：仅重建内存上下文，不再二次落库。
func (c *Conversation) AddMessage(content, accountNo string, isUser, persist bool) error {
	msg := Message{
		ID:        id.GenerateUUID(),
		SessionID: c.sessionID,
		AccountNo: accountNo,
		Content:   content,
		IsUser:    isUser,
	}
	// 统一加锁，避免同步与流式路径并发追加产生竞态。
	c.mu.Lock()
	c.messages = append(c.messages, msg)
	c.mu.Unlock()

	if persist && c.sink != nil {
		return c.sink.Save(msg)
	}
	return nil
}

// Messages 返回消息历史的拷贝，避免外部修改内部切片。
func (c *Conversation) Messages() []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Message, len(c.messages))
	copy(out, c.messages)
	return out
}

// Generate 追加用户问题、驱动模型生成同步回复，并把回复写回历史。
//
// filter 为 RAG 检索的元数据过滤范围；非检索模型会忽略它。
// 通过 WithRetrieveFilter 携带进 ctx，由检索增强路径取出，避免修改通用 Model 端口签名。
func (c *Conversation) Generate(ctx context.Context, accountNo, question string, filter RAGFilter) (string, error) {
	if err := c.AddMessage(question, accountNo, true, true); err != nil {
		return "", err
	}

	// 把过滤意图塞进 ctx，检索增强路径从 ctx 取出；非检索模型对 ctx 内的 filter 无感。
	ctx = WithRetrieveFilter(ctx, filter)
	content, err := c.model.Generate(ctx, c.Messages())
	if err != nil {
		return "", err
	}

	if err := c.AddMessage(content, accountNo, false, true); err != nil {
		return "", err
	}
	return content, nil
}

// Stream 追加用户问题、驱动模型流式生成，分片透传给 cb，并把完整回复写回历史。
//
// filter 为 RAG 检索的元数据过滤范围；通过 WithRetrieveFilter 携带进 ctx。
func (c *Conversation) Stream(ctx context.Context, accountNo, question string, filter RAGFilter, cb StreamCallback) (string, error) {
	if err := c.AddMessage(question, accountNo, true, true); err != nil {
		return "", err
	}

	// 把过滤意图塞进 ctx，检索增强路径从 ctx 取出；非检索模型对 ctx 内的 filter 无感。
	ctx = WithRetrieveFilter(ctx, filter)
	content, err := c.model.Stream(ctx, c.Messages(), cb)
	if err != nil {
		return "", err
	}

	if err := c.AddMessage(content, accountNo, false, true); err != nil {
		return "", err
	}
	return content, nil
}
