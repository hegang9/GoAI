// Package rabbitmq 是消息队列适配层：仅承载与 RabbitMQ 的传输细节（连接、发布、消费）。
//
// 业务语义（消息如何持久化）不在本包内：发布侧通过 Publisher 实现 chat.MessageSink，
// 消费侧通过 Consumer 把解码后的领域消息回调给上层处理函数，从而与 dao/model 解耦。
package rabbitmq

import (
	"fmt"

	"GopherAI/pkg/logger"

	"github.com/streadway/amqp"
)

// Config 描述建立 RabbitMQ 连接所需的参数。
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Vhost    string
	// Queue 队列名（Work 模式下既是队列名也是路由键）。
	Queue string
}

// Client 封装一次 RabbitMQ 连接与其 Channel，提供发布/消费/关闭能力。
type Client struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   string
}

// Connect 建立 RabbitMQ 连接、打开 Channel 并声明队列。
func Connect(cfg Config) (*Client, error) {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/%s",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Vhost)
	logger.Debug("RabbitMQ connecting", "url", url)

	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial failed: %w", err)
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq open channel failed: %w", err)
	}
	// 声明队列（幂等）。参数：队列名、非持久化、非排他、非自动删除、无额外参数。
	if _, err := channel.QueueDeclare(cfg.Queue, false, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq declare queue failed: %w", err)
	}
	return &Client{conn: conn, channel: channel, queue: cfg.Queue}, nil
}

// Publish 向队列发送一条消息（默认交换机，路由键即队列名）。
func (c *Client) Publish(body []byte) error {
	return c.channel.Publish("", c.queue, false, false, amqp.Publishing{
		ContentType: "text/plain",
		Body:        body,
	})
}

// Consume 启动消费循环，对每条消息调用 handle。
//
// 注意：当前使用 autoAck=true（消息一旦投递即确认）。因为消息最终会持久化到 MySQL，
// 即便偶发丢失也可从 DB 恢复；对可靠性有更高要求时可改为手动 ACK。
func (c *Client) Consume(handle func(body []byte) error) {
	msgs, err := c.channel.Consume(c.queue, "", true, false, false, false, nil)
	if err != nil {
		logger.Error("rabbitmq consume failed", "queue", c.queue, "err", err)
		return
	}
	for msg := range msgs {
		if err := handle(msg.Body); err != nil {
			logger.Error("rabbitmq handle message failed", "err", err)
		}
	}
}

// Close 关闭 Channel 与底层 Connection，未初始化时安全返回。
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			return err
		}
	}
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return err
		}
	}
	return nil
}
