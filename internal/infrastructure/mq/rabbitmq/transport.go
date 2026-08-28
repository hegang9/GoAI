// Package rabbitmq 承载 RabbitMQ 的连接、发布、消费与确认细节。
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"GopherAI/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Config 只描述 RabbitMQ TCP 连接；延迟拓扑由 DelayConfig 单独声明。
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Vhost    string
}

// Client 复用一条 TCP Connection，并隔离正常发布和组级死信发布的 Channel。
type Client struct {
	conn *amqp.Connection

	publishChannel *amqp.Channel
	publishMu      sync.Mutex
	publishReturns <-chan amqp.Return

	deadLetterChannel *amqp.Channel
	deadLetterMu      sync.Mutex
	deadLetterReturns <-chan amqp.Return
}

// Connect 建立连接并初始化两个启用 publisher confirm 的长期发布 Channel。
func Connect(cfg Config) (*Client, error) {
	if cfg.Vhost == "" {
		cfg.Vhost = "/"
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	uri := amqp.URI{
		Scheme:   "amqp",
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.Username,
		Password: cfg.Password,
		Vhost:    cfg.Vhost,
	}
	logger.Info("rabbitmq connecting", "host", cfg.Host, "port", cfg.Port, "vhost", cfg.Vhost)

	conn, err := amqp.Dial(uri.String())
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial failed: %w", err)
	}
	connected := false
	defer func() {
		if !connected {
			_ = conn.Close()
		}
	}()

	publishChannel, publishReturns, err := openConfirmChannel(conn, "business publish")
	if err != nil {
		return nil, err
	}
	deadLetterChannel, deadLetterReturns, err := openConfirmChannel(conn, "dead letter publish")
	if err != nil {
		return nil, err
	}

	client := &Client{
		conn:              conn,
		publishChannel:    publishChannel,
		publishReturns:    publishReturns,
		deadLetterChannel: deadLetterChannel,
		deadLetterReturns: deadLetterReturns,
	}
	connected = true
	logger.Info("rabbitmq connected", "host", cfg.Host, "port", cfg.Port, "vhost", cfg.Vhost)
	return client, nil
}

func openConfirmChannel(
	conn *amqp.Connection,
	name string,
) (*amqp.Channel, <-chan amqp.Return, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("rabbitmq open %s channel: %w", name, err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, nil, fmt.Errorf("rabbitmq enable %s confirm: %w", name, err)
	}
	return channel, channel.NotifyReturn(make(chan amqp.Return, 1)), nil
}

// publishConfirmed 使用 mandatory 和 publisher confirm 可靠发布一条消息。
// 调用方必须按 Channel 持有互斥锁，保证 confirm 和 basic.return 与当前消息对应。
func publishConfirmed(
	ctx context.Context,
	channel *amqp.Channel,
	returns <-chan amqp.Return,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	if ctx == nil {
		return errors.New("rabbitmq publish context is nil")
	}
	if channel == nil || channel.IsClosed() {
		return errors.New("rabbitmq publish channel is not available")
	}
	if err := discardStaleReturns(returns); err != nil {
		return err
	}

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		routingKey,
		true,
		false,
		message,
	)
	if err != nil {
		return fmt.Errorf("rabbitmq publish outcome is unknown: %w", err)
	}
	if confirmation == nil {
		return errors.New("rabbitmq publish outcome is unknown: publisher confirm is unavailable")
	}

	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("rabbitmq publish outcome is unknown: wait confirm: %w", err)
	}
	if !acked {
		if channel.IsClosed() {
			return errors.New("rabbitmq publish outcome is unknown: channel closed before confirm")
		}
		return errors.New("rabbitmq publish was negatively acknowledged")
	}

	select {
	case returned, ok := <-returns:
		if !ok {
			return errors.New("rabbitmq publish return listener is closed")
		}
		return fmt.Errorf(
			"rabbitmq publish returned: code=%d text=%s exchange=%s routing_key=%s",
			returned.ReplyCode,
			returned.ReplyText,
			returned.Exchange,
			returned.RoutingKey,
		)
	default:
		return nil
	}
}

// discardStaleReturns 丢弃上一次结果未知的发布可能晚到的 return。
func discardStaleReturns(returns <-chan amqp.Return) error {
	if returns == nil {
		return errors.New("rabbitmq publish return listener is not initialized")
	}
	for {
		select {
		case _, ok := <-returns:
			if !ok {
				return errors.New("rabbitmq publish return listener is closed")
			}
			logger.Warn("rabbitmq discarded stale publish return")
		default:
			return nil
		}
	}
}

func validateConfig(cfg Config) error {
	switch {
	case cfg.Host == "":
		return errors.New("rabbitmq host is empty")
	case cfg.Port <= 0:
		return errors.New("rabbitmq port must be greater than zero")
	case cfg.Username == "":
		return errors.New("rabbitmq username is empty")
	default:
		return nil
	}
}

// Close 按 Channel 到 TCP Connection 的顺序释放资源。
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	var errs []error
	channels := []struct {
		name string
		ch   *amqp.Channel
	}{
		{name: "dead letter", ch: c.deadLetterChannel},
		{name: "business publish", ch: c.publishChannel},
	}
	for _, channel := range channels {
		if channel.ch == nil || channel.ch.IsClosed() {
			continue
		}
		if err := channel.ch.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close rabbitmq %s channel: %w", channel.name, err))
		}
	}
	if c.conn != nil && !c.conn.IsClosed() {
		if err := c.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close rabbitmq connection: %w", err))
		}
	}
	return errors.Join(errs...)
}
