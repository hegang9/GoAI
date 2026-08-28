package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	delaydomain "GopherAI/internal/domain/delay"
	"GopherAI/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DispatcherConsumer 消费 Dispatcher Inbox，并把 Delivery 转交给应用层 Dispatcher。
type DispatcherConsumer struct {
	// Dispatcher 使用独立 Channel，避免和普通业务消费共享 delivery tag、QoS 与故障生命周期。
	channel *amqp.Channel
	closes  <-chan *amqp.Error
	queue   string

	// bootstrap 注入 dispatcher.Submit，避免基础设施层反向依赖应用层。
	submit func(context.Context, delaydomain.Task, func() error) error
}

// NewDispatcherConsumer 创建 Dispatcher Inbox 专用消费者。
func NewDispatcherConsumer(
	client *Client,
	queue string,
	prefetchCount int,
	submit func(context.Context, delaydomain.Task, func() error) error,
) (*DispatcherConsumer, error) {
	if queue == "" {
		return nil, errors.New("new dispatcher consumer: queue is empty")
	}
	if prefetchCount <= 0 {
		return nil, errors.New("new dispatcher consumer: prefetch count must be positive")
	}
	if submit == nil {
		return nil, errors.New("new dispatcher consumer: submit callback is nil")
	}
	if client == nil || client.conn == nil || client.conn.IsClosed() {
		return nil, errors.New("new dispatcher consumer: RabbitMQ client is unavailable")
	}

	channel, err := client.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("new dispatcher consumer: open channel: %w", err)
	}

	initialized := false
	defer func() {
		if !initialized {
			_ = channel.Close()
		}
	}()

	// Prefetch 限制时间轮、ready 和正在发布的任务对应的未 ACK Delivery 总量。
	if err := channel.Qos(prefetchCount, 0, false); err != nil {
		return nil, fmt.Errorf("new dispatcher consumer: configure QoS: %w", err)
	}

	consumer := &DispatcherConsumer{
		channel: channel,
		closes:  channel.NotifyClose(make(chan *amqp.Error, 1)),
		queue:   queue,
		submit:  submit,
	}
	initialized = true
	return consumer, nil
}

// Run 持续消费 Dispatcher Inbox；Context 取消属于正常退出，其他错误交给组合根处理。
func (c *DispatcherConsumer) Run(ctx context.Context) error {
	if c == nil {
		return errors.New("run dispatcher consumer: consumer is nil")
	}
	if ctx == nil {
		return errors.New("run dispatcher consumer: context is nil")
	}
	if c.channel == nil || c.channel.IsClosed() {
		return errors.New("run dispatcher consumer: channel is unavailable")
	}
	if c.closes == nil {
		return errors.New("run dispatcher consumer: close listener is unavailable")
	}
	if c.submit == nil {
		return errors.New("run dispatcher consumer: submit callback is nil")
	}
	// Run 是一次性生命周期；即使 consumer 注册失败，也要释放专用 Channel。
	defer func() {
		if err := c.Close(); err != nil {
			logger.Error("close dispatcher consumer failed", "queue", c.queue, "err", err)
		}
	}()

	deliveries, err := c.channel.ConsumeWithContext(
		ctx,
		c.queue,
		"",    // 由客户端生成 consumer tag。
		false, // 手动 ACK，最终发布成功前 Delivery 始终由 Inbox 持有。
		false, // 允许同一 Queue 部署多个消费者实例。
		false, // RabbitMQ 不支持 no-local 语义。
		false, // 等待 Broker 确认 consumer 注册完成。
		nil,
	)
	if err != nil {
		return fmt.Errorf("consume dispatcher queue %q: %w", c.queue, err)
	}

	logger.Info("dispatcher consumer started", "queue", c.queue)
	defer logger.Info("dispatcher consumer stopped", "queue", c.queue)

	for delivery := range deliveries {
		// ConsumeWithContext 取消后继续读空客户端缓存，但不再向 Dispatcher 提交新任务。
		if ctx.Err() != nil {
			continue
		}

		if err := c.submitDelivery(ctx, delivery); err != nil {
			if ctx.Err() != nil {
				continue
			}
			return err
		}
	}

	if ctx.Err() != nil {
		return nil
	}

	// Delivery 流异常结束时，优先返回 Broker 发送的 Channel 错误。
	select {
	case channelErr, ok := <-c.closes:
		if ok && channelErr != nil {
			return fmt.Errorf("dispatcher consumer channel closed: %w", channelErr)
		}
	default:
	}
	return errors.New("dispatcher consumer delivery stream closed unexpectedly")
}

// submitDelivery 解码任务，并把原 Delivery 的单条 ACK 操作交给 Dispatcher 保存。
func (c *DispatcherConsumer) submitDelivery(ctx context.Context, delivery amqp.Delivery) error {
	var envelope delayEnvelope
	if err := json.Unmarshal(delivery.Body, &envelope); err != nil {
		return fmt.Errorf("decode dispatcher delivery %q: %w", delivery.MessageId, err)
	}

	task, err := decodeTask(envelope)
	if err != nil {
		return fmt.Errorf("restore dispatcher task %q: %w", envelope.TaskID, err)
	}
	if err := task.Validate(); err != nil {
		return fmt.Errorf("validate dispatcher task %q: %w", task.ID, err)
	}

	// Dispatcher 只会在 FinalPublisher 成功后调用 ACK；Consumer 本身绝不提前确认。
	if err := c.submit(ctx, task, func() error {
		return delivery.Ack(false)
	}); err != nil {
		return fmt.Errorf("submit dispatcher task %q: %w", task.ID, err)
	}
	return nil
}

// Close 关闭专用消费 Channel；该 Channel 上所有未 ACK Delivery 将由 RabbitMQ 重新入队。
func (c *DispatcherConsumer) Close() error {
	if c == nil || c.channel == nil || c.channel.IsClosed() {
		return nil
	}
	if err := c.channel.Close(); err != nil {
		return fmt.Errorf("close dispatcher consumer channel: %w", err)
	}
	return nil
}
