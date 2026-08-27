package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"

	"GopherAI/internal/domain/chat"
	"GopherAI/pkg/logger"
)

const (
	// payloadSchemaVersion 是聊天消息 JSON 载荷版本。
	payloadSchemaVersion = 1
	// messageTopicHeader 和 retryAttemptHeader 是新延迟重试链路的稳定元数据。
	messageTopicHeader = "x-goai-topic"
	retryAttemptHeader = "x-retry-attempt"
)

// payload 是消息在队列中传输的 JSON 载荷，独立于数据库模型，避免持久化字段污染消息体。
type payload struct {
	SchemaVersion int    `json:"schema_version"`
	MessageID     string `json:"message_id"`
	SessionID     string `json:"session_id"`
	Content       string `json:"content"`
	AccountNo     string `json:"account_no"`
	IsUser        bool   `json:"is_user"`
}

// Publisher 通过 RabbitMQ 实现 domain/chat.MessageSink 端口：
// 会话聚合产生的消息经此异步发布到队列，再由 Consumer 落库。
type Publisher struct {
	client *Client
}

// NewPublisher 创建消息发布器。
func NewPublisher(client *Client) *Publisher {
	return &Publisher{client: client}
}

// 编译期断言：Publisher 必须满足领域消息持久化端口。
var _ chat.MessageSink = (*Publisher)(nil)

// Save 将领域消息序列化为 JSON 并发布到队列。
func (p *Publisher) Save(msg chat.Message) error {
	data, err := json.Marshal(payload{
		SchemaVersion: payloadSchemaVersion,
		MessageID:     msg.ID,
		SessionID:     msg.SessionID,
		Content:       msg.Content,
		AccountNo:     msg.AccountNo,
		IsUser:        msg.IsUser,
	})
	if err != nil {
		return fmt.Errorf("encode payload failed: %w", err)
	}
	if err := p.client.Publish(msg.ID, data); err != nil {
		logger.Error("Publisher Save publish failed", "sessionID", msg.SessionID, "err", err)
		return err
	}
	return nil
}

// Consumer 从队列读取消息、解码为领域消息并交给业务处理函数（典型为落库）。
type Consumer struct {
	client *Client
	// handle 业务处理函数，由 bootstrap 注入（如通过 MessageRepository 持久化）。
	handle func(ctx context.Context, msg chat.Message) error
}

// NewConsumer 创建消息消费者。
func NewConsumer(client *Client, handle func(ctx context.Context, msg chat.Message) error) *Consumer {
	return &Consumer{client: client, handle: handle}
}

// Start 在独立 goroutine 中启动消费循环。
func (c *Consumer) Start() {
	go c.client.Consume(c.decode)
}

// decodeMessage 将稳定的队列载荷转换为聊天领域消息。
func decodeMessage(body []byte) (chat.Message, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return chat.Message{}, permanentError(fmt.Errorf("decode payload failed: %w", err))
	}
	if p.SchemaVersion != payloadSchemaVersion {
		return chat.Message{}, permanentError(fmt.Errorf(
			"unsupported payload schema version: %d",
			p.SchemaVersion,
		))
	}
	if p.MessageID == "" {
		return chat.Message{}, permanentError(fmt.Errorf("rabbitmq payload message_id is empty"))
	}

	return chat.Message{
		ID:        p.MessageID,
		SessionID: p.SessionID,
		Content:   p.Content,
		AccountNo: p.AccountNo,
		IsUser:    p.IsUser,
	}, nil
}

// decode 解码队列消息并交给业务处理函数。
func (c *Consumer) decode(body []byte) error {
	message, err := decodeMessage(body)
	if err != nil {
		return err
	}
	return c.handle(context.Background(), message)
}
