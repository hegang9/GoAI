// FinalPublisher 将成熟的延迟任务还原为业务消息并发布到最终目标。
package rabbitmq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"

	amqp "github.com/rabbitmq/amqp091-go"
)

type FinalPublisher struct {
	channel finalPublishChannel
	returns <-chan amqp.Return
	closes  <-chan *amqp.Error

	mu     sync.Mutex
	config FinalPublisherConfig

	topics         map[string]struct{}
	consumerGroups map[string]struct{}
}

type finalConfirmation interface {
	WaitContext(ctx context.Context) (bool, error)
}

type finalPublishChannel interface {
	PublishWithDeferredConfirmWithContext(
		ctx context.Context,
		exchange string,
		key string,
		mandatory bool,
		immediate bool,
		msg amqp.Publishing,
	) (finalConfirmation, error)
	IsClosed() bool
	Close() error
}

type amqpFinalPublishChannel struct {
	channel *amqp.Channel
}

func (c *amqpFinalPublishChannel) PublishWithDeferredConfirmWithContext(
	ctx context.Context,
	exchange string,
	key string,
	mandatory bool,
	immediate bool,
	msg amqp.Publishing,
) (finalConfirmation, error) {
	return c.channel.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		key,
		mandatory,
		immediate,
		msg,
	)
}

func (c *amqpFinalPublishChannel) IsClosed() bool {
	return c == nil || c.channel == nil || c.channel.IsClosed()
}

func (c *amqpFinalPublishChannel) Close() error {
	if c == nil || c.channel == nil {
		return nil
	}
	return c.channel.Close()
}

type FinalPublisherConfig struct {
	// TopicExchange 承载正常 Topic 广播，RedriveExchange 用于精确回投消费者组。
	TopicExchange   string
	RedriveExchange string
	// ConfirmTimeout 限制一次发布等待 Broker confirm 的时间。
	ConfirmTimeout time.Duration

	Topics         []string
	ConsumerGroups []string
}

var _ delay.FinalPublisher = (*FinalPublisher)(nil)

func NewFinalPublisher(client *Client, config FinalPublisherConfig) (*FinalPublisher, error) {
	if client == nil || client.conn == nil {
		return nil, errors.New("new final publisher: rabbitmq client is nil")
	}

	config.TopicExchange = strings.TrimSpace(config.TopicExchange)
	config.RedriveExchange = strings.TrimSpace(config.RedriveExchange)
	if err := validateFinalPublisherConfig(config); err != nil {
		return nil, fmt.Errorf("new final publisher: %w", err)
	}

	topics, err := slice2map(config.Topics, "Topics")
	if err != nil {
		return nil, fmt.Errorf("new final publisher: %w", err)
	}

	consumerGroups, err := slice2map(config.ConsumerGroups, "ConsumerGroups")
	if err != nil {
		return nil, fmt.Errorf("new final publisher: %w", err)
	}

	channel, err := client.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open final publisher channel: %w", err)
	}

	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf(
			"enable final publisher confirm: %w",
			err,
		)
	}

	returns := channel.NotifyReturn(
		make(chan amqp.Return, 1),
	)
	// NotifyClose 必须使用有缓冲监听器，避免异常关闭时阻塞 amqp091-go 的分发协程。
	closes := channel.NotifyClose(make(chan *amqp.Error, 1))

	return &FinalPublisher{
		channel:        &amqpFinalPublishChannel{channel: channel},
		returns:        returns,
		closes:         closes,
		config:         config,
		topics:         topics,
		consumerGroups: consumerGroups,
	}, nil
}

func validateFinalPublisherConfig(config FinalPublisherConfig) error {
	switch {
	case strings.TrimSpace(config.TopicExchange) == "":
		return errors.New("final publisher topic exchange is empty")

	case strings.TrimSpace(config.RedriveExchange) == "":
		return errors.New("final publisher redrive exchange is empty")

	case strings.TrimSpace(config.TopicExchange) == strings.TrimSpace(config.RedriveExchange):
		return errors.New(
			"final publisher topic and redrive exchanges must differ",
		)

	case config.ConfirmTimeout <= 0:
		return errors.New(
			"final publisher confirm timeout must be positive",
		)

	case len(config.Topics) == 0:
		return errors.New("final publisher topics are empty")

	case len(config.ConsumerGroups) == 0:
		return errors.New("final publisher consumer groups are empty")

	default:
		return nil
	}
}

func slice2map(values []string, field string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s contains empty value", field)
		}
		result[value] = struct{}{}
	}

	return result, nil
}

func (p *FinalPublisher) route(task delay.Task) (string, string, error) {
	switch task.Target.Kind {
	case messageDomain.TargetTopic:
		if _, ok := p.topics[task.Message.Topic]; !ok {
			return "", "", fmt.Errorf(
				"final publish topic %q is not allowed",
				task.Message.Topic,
			)
		}
		return p.config.TopicExchange, task.Message.Topic, nil

	case messageDomain.TargetConsumerGroup:
		if _, ok := p.consumerGroups[task.Target.ConsumerGroup]; !ok {
			return "", "", fmt.Errorf(
				"final publish consumer group %q is not configured",
				task.Target.ConsumerGroup,
			)
		}
		return p.config.RedriveExchange, task.Target.ConsumerGroup, nil

	default:
		return "", "", fmt.Errorf(
			"final publish target kind %d is unsupported",
			task.Target.Kind,
		)
	}
}

func buildFinalPublishing(task delay.Task) amqp.Publishing {
	headers := make(amqp.Table, len(task.Message.Headers)+2)
	for key, value := range task.Message.Headers {
		headers[key] = value
	}
	headers["x-goai-topic"] = task.Message.Topic
	headers["x-retry-attempt"] = int64(task.RetryAttempt)

	return amqp.Publishing{
		Headers:       headers,
		DeliveryMode:  amqp.Persistent,
		MessageId:     task.Message.ID,
		CorrelationId: task.ID,
		Timestamp:     task.Message.Timestamp.UTC(),
		Type:          task.Message.Topic,
		Body:          bytes.Clone(task.Message.Body),
	}
}

// Publish 只有在 Broker ACK 且 mandatory publish 未被退回时才返回 nil。
// 返回错误时 Dispatcher 必须保留原 Inbox Delivery 的未 ACK 状态，等待 RabbitMQ 重投。
func (p *FinalPublisher) Publish(ctx context.Context, task delay.Task) error {
	if p == nil {
		return errors.New("final publisher is nil")
	}
	if ctx == nil {
		return errors.New("final publish context is nil")
	}
	if err := task.Validate(); err != nil {
		return fmt.Errorf("validate final publish task %q: %w", task.ID, err)
	}

	exchange, routingKey, err := p.route(task)
	if err != nil {
		return fmt.Errorf("route final publish task %q: %w", task.ID, err)
	}
	message := buildFinalPublishing(task)

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf(
			"final publish task %q cancelled before publish: %w",
			task.ID,
			err,
		)
	}
	if p.channel == nil || p.channel.IsClosed() {
		return p.channelUnavailableError()
	}
	if p.returns == nil {
		return errors.New("final publisher return listener is unavailable")
	}

	publishCtx, cancel := context.WithTimeout(ctx, p.config.ConfirmTimeout)
	defer cancel()

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		publishCtx,
		exchange,
		routingKey,
		true,
		false,
		message,
	)
	if err != nil {
		p.invalidateChannel()
		return fmt.Errorf(
			"publish final task %q has unknown outcome: %w",
			task.ID,
			err,
		)
	}
	if confirmation == nil {
		p.invalidateChannel()
		return fmt.Errorf(
			"publish final task %q has unknown outcome: publisher confirm is unavailable",
			task.ID,
		)
	}

	acked, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		p.invalidateChannel()
		return fmt.Errorf(
			"wait final task %q confirm has unknown outcome: %w",
			task.ID,
			err,
		)
	}
	if !acked {
		// amqp091-go 在 Channel 关闭时也会用 acked=false 唤醒等待者。
		if p.channel.IsClosed() {
			p.channel = nil
			return fmt.Errorf(
				"final task %q confirm interrupted; outcome unknown: %w",
				task.ID,
				p.channelUnavailableError(),
			)
		}

		p.invalidateChannel()
		return fmt.Errorf("final task %q was negatively acknowledged", task.ID)
	}

	select {
	case returned, ok := <-p.returns:
		if !ok {
			p.invalidateChannel()
			return fmt.Errorf(
				"final task %q return listener closed; outcome unknown",
				task.ID,
			)
		}
		if returned.MessageId != task.Message.ID || returned.CorrelationId != task.ID {
			p.invalidateChannel()
			return fmt.Errorf(
				"final task %q received unrelated mandatory return; outcome unknown",
				task.ID,
			)
		}
		return fmt.Errorf(
			"final task %q was returned: code=%d text=%s exchange=%s routing_key=%s",
			task.ID,
			returned.ReplyCode,
			returned.ReplyText,
			returned.Exchange,
			returned.RoutingKey,
		)

	default:
		return nil
	}
}

func (p *FinalPublisher) channelUnavailableError() error {
	if p.closes != nil {
		select {
		case closeErr, ok := <-p.closes:
			if ok && closeErr != nil {
				return fmt.Errorf("final publisher channel is unavailable: %w", closeErr)
			}
		default:
		}
	}
	return errors.New("final publisher channel is unavailable")
}

// invalidateChannel 防止结果未知或 confirm/return 关联异常后继续复用已污染的 Channel。
// 调用方必须已经持有 p.mu。
func (p *FinalPublisher) invalidateChannel() {
	if p.channel == nil {
		return
	}
	_ = p.channel.Close()
	p.channel = nil
}

func (p *FinalPublisher) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.channel == nil {
		return nil
	}
	channel := p.channel
	p.channel = nil
	if channel.IsClosed() {
		return nil
	}
	if err := channel.Close(); err != nil {
		return fmt.Errorf("close final publisher channel: %w", err)
	}
	return nil
}
