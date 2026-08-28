package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"GopherAI/internal/domain/chat"
	"GopherAI/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// payloadSchemaVersion 是聊天消息 JSON 载荷版本。
	payloadSchemaVersion = 1
	// messageTopicHeader 和 retryAttemptHeader 是延迟重试链路的稳定元数据。
	messageTopicHeader = "x-goai-topic"
	retryAttemptHeader = "x-retry-attempt"
)

// payload 是消息在队列中传输的 JSON 载荷，独立于数据库模型。
type payload struct {
	SchemaVersion int    `json:"schema_version"`
	MessageID     string `json:"message_id"`
	SessionID     string `json:"session_id"`
	Content       string `json:"content"`
	AccountNo     string `json:"account_no"`
	IsUser        bool   `json:"is_user"`
}

// Publisher 通过 RabbitMQ Topic Exchange 实现聊天消息异步持久化端口。
type Publisher struct {
	client         *Client
	exchange       string
	topic          string
	confirmTimeout time.Duration
}

// NewPublisher 创建直接发布到业务 Topic Exchange 的消息发布器。
func NewPublisher(
	client *Client,
	exchange string,
	topic string,
	confirmTimeout time.Duration,
) (*Publisher, error) {
	if err := validatePublisherConfig(exchange, topic, confirmTimeout); err != nil {
		return nil, fmt.Errorf("new publisher: %w", err)
	}
	if client == nil || client.publishChannel == nil || client.publishChannel.IsClosed() {
		return nil, errors.New("new publisher: RabbitMQ client is unavailable")
	}
	return &Publisher{
		client:         client,
		exchange:       strings.TrimSpace(exchange),
		topic:          strings.TrimSpace(topic),
		confirmTimeout: confirmTimeout,
	}, nil
}

// 编译期断言：Publisher 必须满足领域消息持久化端口。
var _ chat.MessageSink = (*Publisher)(nil)

func validatePublisherConfig(exchange string, topic string, confirmTimeout time.Duration) error {
	switch {
	case strings.TrimSpace(exchange) == "":
		return errors.New("publisher exchange is empty")
	case strings.TrimSpace(topic) == "":
		return errors.New("publisher topic is empty")
	case confirmTimeout <= 0:
		return errors.New("publisher confirm timeout must be positive")
	default:
		return nil
	}
}

// buildTopicPublishing 构造初次业务投递格式，业务重试次数从 0 开始。
func buildTopicPublishing(
	msg chat.Message,
	topic string,
	timestamp time.Time,
) (amqp.Publishing, error) {
	if strings.TrimSpace(msg.ID) == "" {
		return amqp.Publishing{}, errors.New("build topic publishing: message ID is empty")
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return amqp.Publishing{}, errors.New("build topic publishing: topic is empty")
	}
	if timestamp.IsZero() {
		return amqp.Publishing{}, errors.New("build topic publishing: timestamp is zero")
	}

	data, err := json.Marshal(payload{
		SchemaVersion: payloadSchemaVersion,
		MessageID:     msg.ID,
		SessionID:     msg.SessionID,
		Content:       msg.Content,
		AccountNo:     msg.AccountNo,
		IsUser:        msg.IsUser,
	})
	if err != nil {
		return amqp.Publishing{}, fmt.Errorf("encode payload: %w", err)
	}

	return amqp.Publishing{
		Headers: amqp.Table{
			messageTopicHeader: topic,
			retryAttemptHeader: int64(0),
		},
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		MessageId:    msg.ID,
		Timestamp:    timestamp.UTC(),
		Type:         topic,
		Body:         data,
	}, nil
}

// Save 只有在 Broker confirm 成功且 mandatory publish 未被退回后才返回 nil。
func (p *Publisher) Save(msg chat.Message) error {
	if p == nil || p.client == nil {
		return errors.New("publisher is unavailable")
	}
	message, err := buildTopicPublishing(msg, p.topic, time.Now())
	if err != nil {
		return err
	}

	p.client.publishMu.Lock()
	defer p.client.publishMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), p.confirmTimeout)
	defer cancel()
	if err := publishConfirmed(
		ctx,
		p.client.publishChannel,
		p.client.publishReturns,
		p.exchange,
		p.topic,
		message,
	); err != nil {
		logger.Error("Publisher Save publish failed", "sessionID", msg.SessionID, "err", err)
		return fmt.Errorf("publish topic message %q: %w", msg.ID, err)
	}
	return nil
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
		return chat.Message{}, permanentError(errors.New("rabbitmq payload message_id is empty"))
	}

	return chat.Message{
		ID:        p.MessageID,
		SessionID: p.SessionID,
		Content:   p.Content,
		AccountNo: p.AccountNo,
		IsUser:    p.IsUser,
	}, nil
}
