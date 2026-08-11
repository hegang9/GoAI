package message

import (
	"bytes"
	"errors"
	"maps"
	"strings"
	"time"
)

var ErrInvalidMessage = errors.New("invalid message")

// Message 表示跨延迟调度与消费重试保持不变的业务消息。
type Message struct {
	ID        string
	Topic     string
	Headers   map[string]string
	Body      []byte
	Timestamp time.Time
}

// New 创建业务消息，并复制可变字段，避免调用方在创建后篡改消息内容。
func New(
	id string,
	topic string,
	headers map[string]string,
	body []byte,
	timestamp time.Time,
) (Message, error) {
	message := Message{
		ID:        id,
		Topic:     topic,
		Headers:   maps.Clone(headers),
		Body:      bytes.Clone(body),
		Timestamp: timestamp.UTC(),
	}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

// Validate 校验业务消息的稳定标识、主题、载荷和发生时间。
func (m Message) Validate() error {
	switch {
	case strings.TrimSpace(m.ID) == "":
		return errors.Join(ErrInvalidMessage, errors.New("message id is empty"))
	case strings.TrimSpace(m.Topic) == "":
		return errors.Join(ErrInvalidMessage, errors.New("message topic is empty"))
	case m.Body == nil:
		return errors.Join(ErrInvalidMessage, errors.New("message body is nil"))
	case m.Timestamp.IsZero():
		return errors.Join(ErrInvalidMessage, errors.New("message timestamp is zero"))
	default:
		return nil
	}
}

// Clone 返回不共享 Headers 和 Body 底层数据的消息副本。
func (m Message) Clone() Message {
	m.Headers = maps.Clone(m.Headers)
	m.Body = bytes.Clone(m.Body)
	m.Timestamp = m.Timestamp.UTC()
	return m
}
