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
	"GopherAI/internal/domain/message"

	amqp "github.com/rabbitmq/amqp091-go"
)

// FinalPublisher 将成熟的延迟任务还原为业务消息，并可靠发布到最终目标。
type FinalPublisher struct {
	// 发布、confirm 和 mandatory return 都属于同一个 Channel 的状态。
	channel *amqp.Channel
	returns <-chan amqp.Return
	// closes 保存 Broker 主动关闭 Channel 时的具体原因。
	closes <-chan *amqp.Error

	// 同一 Channel 上一次只允许一条待确认消息，避免 confirm 与 return 关联错位。
	mu     sync.Mutex
	config FinalPublisherConfig

	// 白名单同时承担配置校验，避免把任务发布到未声明的拓扑。
	topics         map[string]struct{}
	consumerGroups map[string]struct{}
}

// FinalPublisherConfig 描述最终发布所需的 Exchange、目标白名单和确认超时。
type FinalPublisherConfig struct {
	// TopicExchange 承载正常 Topic 广播，RedriveExchange 用于精确回投消费者组。
	TopicExchange   string
	RedriveExchange string
	// ConfirmTimeout 限制一次发布等待 Broker confirm 的时间。
	ConfirmTimeout time.Duration

	// Topics 和 ConsumerGroups 必须与启动时声明的 RabbitMQ 拓扑保持一致。
	Topics         []string
	ConsumerGroups []string
}

// 编译期确认 RabbitMQ 实现满足领域层端口。
var _ delay.FinalPublisher = (*FinalPublisher)(nil)

// NewFinalPublisher 创建独立发布 Channel，并启用 publisher confirm 和 mandatory return 监听。
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

	// mu 保证最多一条消息等待结果，因此容量 1 足以承接当前消息的异步通知。
	returns := channel.NotifyReturn(
		make(chan amqp.Return, 1),
	)
	closes := channel.NotifyClose(make(chan *amqp.Error, 1))
	return &FinalPublisher{
		channel:        channel,
		returns:        returns,
		closes:         closes,
		config:         config,
		topics:         topics,
		consumerGroups: consumerGroups,
	}, nil
}

// validateFinalPublisherConfig 在建立 Channel 前拒绝不完整或相互冲突的配置。
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

// slice2map 清理配置项并转换为便于路由校验的集合。
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

// route 把领域层逻辑目标映射为 RabbitMQ Exchange 和 routing key。
func (p *FinalPublisher) route(task delay.Task) (string, string, error) {
	switch task.Target.Kind {
	case message.TargetTopic:
		if _, ok := p.topics[task.Message.Topic]; !ok {
			return "", "", fmt.Errorf(
				"final publish topic %q is not allowed",
				task.Message.Topic,
			)
		}
		return p.config.TopicExchange, task.Message.Topic, nil

	case message.TargetConsumerGroup:
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

// buildFinalPublishing 恢复原始业务消息，并补充重试链路所需的元数据。
func buildFinalPublishing(task delay.Task) amqp.Publishing {
	// 复制 Headers 和 Body，避免 AMQP 客户端异步使用期间被领域对象修改。
	headers := make(amqp.Table, len(task.Message.Headers)+2)
	for key, value := range task.Message.Headers {
		headers[key] = value
	}
	headers[messageTopicHeader] = task.Message.Topic
	headers[retryAttemptHeader] = int64(task.RetryAttempt)

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

	// confirm 和 mandatory return 都是 Channel 级事件，必须覆盖完整发布确认周期。
	p.mu.Lock()
	defer p.mu.Unlock()

	// 任务等待锁期间可能已经被 Dispatcher 取消，正式发布前再次检查。
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

	// mandatory=true 让不可路由消息通过 basic.return 返回；immediate 已被 RabbitMQ 废弃。
	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		publishCtx,
		exchange,
		routingKey,
		true,
		false,
		message,
	)
	if err != nil {
		// Publish 返回错误时消息可能已经写入网络，不能断言 Broker 没有接管。
		p.invalidateChannel()
		return fmt.Errorf(
			"publish final task %q has unknown outcome: %w",
			task.ID,
			err,
		)
	}
	if confirmation == nil {
		// 消息可能已经发出，但缺少可等待的确认对象，结果仍然未知。
		p.invalidateChannel()
		return fmt.Errorf(
			"publish final task %q has unknown outcome: publisher confirm is unavailable",
			task.ID,
		)
	}

	acked, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		// 超时或连接中断都不能证明 Broker 未收到消息。
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

		// Channel 仍可用时，acked=false 才表示 Broker 明确 NACK。
		p.invalidateChannel()
		return fmt.Errorf("final task %q was negatively acknowledged", task.ID)
	}

	// 对不可路由的 mandatory 消息，RabbitMQ 会先通知 return listener，再发送 confirm。
	select {
	case returned, ok := <-p.returns:
		if !ok {
			// 监听器关闭后无法排除遗漏 return，因此不能把 ACK 当作最终成功。
			p.invalidateChannel()
			return fmt.Errorf(
				"final task %q return listener closed; outcome unknown",
				task.ID,
			)
		}
		if returned.MessageId != task.Message.ID || returned.CorrelationId != task.ID {
			// 出现不属于当前任务的 return，说明 Channel 事件关联已经不可信。
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
		// Broker ACK 且没有 mandatory return，最终消息系统已经接管消息。
		return nil
	}
}

// channelUnavailableError 尽量附带 Broker 关闭 Channel 的具体原因。
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

// Close 关闭发布 Channel；重复调用是安全的。
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
