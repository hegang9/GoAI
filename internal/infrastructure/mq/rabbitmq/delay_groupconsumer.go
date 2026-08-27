package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	chatDomain "GopherAI/internal/domain/chat"
	messageDomain "GopherAI/internal/domain/message"
	"GopherAI/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// GroupConsumer 消费一个消费者组的业务 Queue。
type GroupConsumer struct {
	// 独立 Channel 隔离 delivery tag、QoS 和故障生命周期。
	channel *amqp.Channel
	closes  <-chan *amqp.Error

	consumerGroup string
	queue         string

	// handle 由 bootstrap 注入，当前 persistence 组对应消息持久化。
	handle func(context.Context, chatDomain.Message) error
}

// NewGroupConsumer 创建消费者组专用 Consumer，并配置未 ACK 窗口。
func NewGroupConsumer(
	client *Client,
	consumerGroup string,
	queue string,
	prefetchCount int,
	handle func(context.Context, chatDomain.Message) error,
) (*GroupConsumer, error) {
	consumerGroup = strings.TrimSpace(consumerGroup)
	queue = strings.TrimSpace(queue)

	switch {
	case consumerGroup == "":
		return nil, errors.New("new group consumer: consumer group is empty")
	case queue == "":
		return nil, errors.New("new group consumer: queue is empty")
	case prefetchCount <= 0:
		return nil, errors.New("new group consumer: prefetch count must be positive")
	case handle == nil:
		return nil, errors.New("new group consumer: handler is nil")
	case client == nil || client.conn == nil || client.conn.IsClosed():
		return nil, errors.New("new group consumer: RabbitMQ client is unavailable")
	}

	channel, err := client.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("new group consumer: open channel: %w", err)
	}

	initialized := false
	defer func() {
		if !initialized {
			_ = channel.Close()
		}
	}()

	// Prefetch 限制该实例同时持有的未 ACK 业务消息数量。
	if err := channel.Qos(prefetchCount, 0, false); err != nil {
		return nil, fmt.Errorf("new group consumer: configure QoS: %w", err)
	}

	consumer := &GroupConsumer{
		channel:       channel,
		closes:        channel.NotifyClose(make(chan *amqp.Error, 1)),
		consumerGroup: consumerGroup,
		queue:         queue,
		handle:        handle,
	}
	initialized = true
	return consumer, nil
}

// Run 持续消费消费者组 Queue；完整重试状态机接入前，处理失败会关闭 Channel 并重新入队。
func (c *GroupConsumer) Run(ctx context.Context) error {
	if c == nil {
		return errors.New("run group consumer: consumer is nil")
	}
	if ctx == nil {
		return errors.New("run group consumer: context is nil")
	}
	if c.channel == nil || c.channel.IsClosed() {
		return errors.New("run group consumer: channel is unavailable")
	}
	if c.closes == nil {
		return errors.New("run group consumer: close listener is unavailable")
	}
	if c.handle == nil {
		return errors.New("run group consumer: handler is nil")
	}

	defer func() {
		if err := c.Close(); err != nil {
			logger.Error(
				"close group consumer failed",
				"consumerGroup", c.consumerGroup,
				"queue", c.queue,
				"err", err,
			)
		}
	}()

	deliveries, err := c.channel.ConsumeWithContext(
		ctx,
		c.queue,
		"",    // 由客户端生成 consumer tag。
		false, // 手动 ACK。
		false, // 允许同一消费者组部署多个实例。
		false, // RabbitMQ 不支持 no-local。
		false, // 等待 Broker 确认 Consumer 注册成功。
		nil,
	)
	if err != nil {
		return fmt.Errorf("consume group queue %q: %w", c.queue, err)
	}

	logger.Info(
		"group consumer started",
		"consumerGroup", c.consumerGroup,
		"queue", c.queue,
	)
	defer logger.Info(
		"group consumer stopped",
		"consumerGroup", c.consumerGroup,
		"queue", c.queue,
	)

	for delivery := range deliveries {
		// Context 取消后不再处理客户端已经缓存的 Delivery。
		if ctx.Err() != nil {
			continue
		}

		if err := c.handleDelivery(ctx, delivery); err != nil {
			if ctx.Err() != nil {
				continue
			}
			return err
		}
	}

	if ctx.Err() != nil {
		return nil
	}

	select {
	case channelErr, ok := <-c.closes:
		if ok && channelErr != nil {
			return fmt.Errorf("group consumer channel closed: %w", channelErr)
		}
	default:
	}
	return errors.New("group consumer delivery stream closed unexpectedly")
}

// deliveryRetryAttempt 读取当前业务重试次数；缺少 Header 表示初次消费。
func deliveryRetryAttempt(headers amqp.Table) (uint32, error) {
	if headers == nil {
		return 0, nil
	}

	raw, exists := headers[retryAttemptHeader]
	if !exists {
		return 0, nil
	}

	var attempt int64
	switch value := raw.(type) {
	case int8:
		attempt = int64(value)
	case int16:
		attempt = int64(value)
	case int32:
		attempt = int64(value)
	case int64:
		attempt = value
	case int:
		attempt = int64(value)
	default:
		return 0, fmt.Errorf("invalid %s type %T", retryAttemptHeader, raw)
	}

	if attempt < 0 || attempt > int64(math.MaxUint32) {
		return 0, fmt.Errorf("invalid %s value %d", retryAttemptHeader, attempt)
	}
	return uint32(attempt), nil
}

// retryMessageHeaders 删除延迟系统元数据，并恢复领域消息支持的字符串 Header。
func retryMessageHeaders(headers amqp.Table) (map[string]string, error) {
	result := make(map[string]string, len(headers))
	for key, raw := range headers {
		if key == messageTopicHeader || key == retryAttemptHeader {
			continue
		}

		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf(
				"retry message header %q has unsupported type %T",
				key,
				raw,
			)
		}
		result[key] = value
	}
	return result, nil
}

// decodeGroupDelivery 同时恢复 Handler 使用的聊天消息和延迟重试使用的稳定业务消息。
func decodeGroupDelivery(
	delivery amqp.Delivery,
) (chatDomain.Message, messageDomain.Message, uint32, error) {
	businessMessage, err := decodeMessage(delivery.Body)
	if err != nil {
		return chatDomain.Message{}, messageDomain.Message{}, 0, err
	}

	if strings.TrimSpace(delivery.MessageId) == "" {
		return chatDomain.Message{}, messageDomain.Message{}, 0,
			permanentError(errors.New("group delivery message_id is empty"))
	}
	if businessMessage.ID != delivery.MessageId {
		return chatDomain.Message{}, messageDomain.Message{}, 0,
			permanentError(fmt.Errorf(
				"group delivery message ID mismatch: body=%q property=%q",
				businessMessage.ID,
				delivery.MessageId,
			))
	}

	topic := strings.TrimSpace(delivery.Type)
	if rawTopic, exists := delivery.Headers[messageTopicHeader]; exists {
		headerTopic, ok := rawTopic.(string)
		if !ok {
			return chatDomain.Message{}, messageDomain.Message{}, 0,
				permanentError(fmt.Errorf(
					"invalid %s type %T",
					messageTopicHeader,
					rawTopic,
				))
		}

		headerTopic = strings.TrimSpace(headerTopic)
		if headerTopic == "" {
			return chatDomain.Message{}, messageDomain.Message{}, 0,
				permanentError(fmt.Errorf("%s is empty", messageTopicHeader))
		}
		if topic != "" && topic != headerTopic {
			return chatDomain.Message{}, messageDomain.Message{}, 0,
				permanentError(fmt.Errorf(
					"group delivery topic mismatch: type=%q header=%q",
					topic,
					headerTopic,
				))
		}
		topic = headerTopic
	}

	attempt, err := deliveryRetryAttempt(delivery.Headers)
	if err != nil {
		return chatDomain.Message{}, messageDomain.Message{}, 0, permanentError(err)
	}
	headers, err := retryMessageHeaders(delivery.Headers)
	if err != nil {
		return chatDomain.Message{}, messageDomain.Message{}, 0, permanentError(err)
	}

	retryMessage, err := messageDomain.New(
		delivery.MessageId,
		topic,
		headers,
		delivery.Body,
		delivery.Timestamp,
	)
	if err != nil {
		return chatDomain.Message{}, messageDomain.Message{}, 0,
			permanentError(fmt.Errorf(
				"restore retry message %q: %w",
				delivery.MessageId,
				err,
			))
	}

	return businessMessage, retryMessage, attempt, nil
}

func (c *GroupConsumer) handleDelivery(ctx context.Context, delivery amqp.Delivery) error {
	businessMessage, retryMessage, currentAttempt, err := decodeGroupDelivery(delivery)
	if err != nil {
		return fmt.Errorf("decode group delivery %q: %w", delivery.MessageId, err)
	}

	if err := c.handle(ctx, businessMessage); err != nil {
		// 下一步在这里按错误类型调用 ScheduleRetry 或发布组级 DLQ。
		return fmt.Errorf(
			"handle group delivery %q: consumer_group=%q retry_message=%q retry_attempt=%d: %w",
			delivery.MessageId,
			c.consumerGroup,
			retryMessage.ID,
			currentAttempt,
			err,
		)
	}

	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("ack group delivery %q: %w", delivery.MessageId, err)
	}
	return nil
}

// Close 关闭专用消费 Channel，未 ACK Delivery 由 RabbitMQ 重新入队。
func (c *GroupConsumer) Close() error {
	if c == nil || c.channel == nil || c.channel.IsClosed() {
		return nil
	}
	if err := c.channel.Close(); err != nil {
		return fmt.Errorf("close group consumer channel: %w", err)
	}
	return nil
}
