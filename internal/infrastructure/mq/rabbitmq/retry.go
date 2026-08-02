package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	rand "math/rand/v2"

	"GopherAI/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// retry.go 实现错误分类、重试档位与抖动计算，以及 retry/DLQ 的可靠发布。

// ProcessingError 显式标记确定性或系统性异常，未包装错误默认按瞬时异常处理。
type FailureKind uint8

// 瞬时、永久、终止三类错误
const (
	FailureTransient FailureKind = iota
	FailurePermanent
	FailureAbort
)

func (kind FailureKind) String() string {
	switch kind {
	case FailureTransient:
		return "transient"
	case FailurePermanent:
		return "permanent"
	case FailureAbort:
		return "abort"
	default:
		return fmt.Sprintf("unknown(%d)", kind)
	}
}

type ProcessingError struct {
	Kind FailureKind
	Err  error
}

func (e *ProcessingError) Error() string {
	return e.Err.Error()
}

func (e *ProcessingError) Unwrap() error {
	return e.Err
}

// 构造函数
func permanentError(err error) error {
	if err == nil {
		return nil
	}
	return &ProcessingError{
		Kind: FailurePermanent,
		Err:  err,
	}
}

func abortError(err error) error {
	if err == nil {
		return nil
	}
	return &ProcessingError{
		Kind: FailureAbort,
		Err:  err,
	}
}

// 错误分类
func classifyError(err error) FailureKind {
	if err == nil {
		return FailureTransient
	}

	var processingErr *ProcessingError
	if errors.As(err, &processingErr) {
		return processingErr.Kind
	}

	// 未明确标记的错误默认作为瞬时异常。
	return FailureTransient
}

// 自定义的重试轮次字段
const retryCountHeader = "x-retry-count"

// RetryTier 描述一档延迟重试队列。
// Queue 和 RoutingKey 必须与 RabbitMQ 控制台中的 binding 完全一致；DelayMs 是基础延迟，
// topology.go 会结合 RetryJitterPercent 计算该队列的最大 TTL。
type RetryTier struct {
	// Queue 是用于暂存重试消息的 durable queue 名称。
	Queue string
	// RoutingKey 用于从 RetryExchange 精确路由到当前重试队列。
	RoutingKey string
	// DelayMs 是当前档位的基础延迟，单位为毫秒。
	DelayMs int
}

// retryCount 兼容amqp解码出的不同整数类型
func retryCount(headers amqp.Table) (int, error) {
	if headers == nil {
		return 0, nil
	}

	value, ok := headers[retryCountHeader]
	if !ok {
		return 0, nil
	}

	var count int

	switch value := value.(type) {
	case int8:
		count = int(value)
	case int16:
		count = int(value)
	case int32:
		count = int(value)
	case int64:
		count = int(value)
	case int:
		count = value
	default:
		return 0, fmt.Errorf(
			"invalid %s type %T",
			retryCountHeader,
			value,
		)
	}

	if count < 0 {
		return 0, fmt.Errorf(
			"invalid negative retry count: %d",
			count,
		)
	}

	return count, nil
}

// copyHeaders 复制 Header，避免直接修改 delivery.Headers
func copyHeaders(src amqp.Table) amqp.Table {
	target := make(amqp.Table, len(src)+8)
	for key, value := range src {
		target[key] = value
	}

	return target
}

// selectRetryTier 重试档位选择
func selectRetryTier(tiers []RetryTier, curRetryCount int) (RetryTier, error) {
	if curRetryCount < 0 || curRetryCount >= len(tiers) {
		return RetryTier{}, fmt.Errorf("invalid retry count: %d", curRetryCount)
	}
	return tiers[curRetryCount], nil
}

// retryDelayMs 抖动计算
func retryDelayMs(baseMs, jitterPercent int) int {
	maxJitterMs := baseMs * jitterPercent / 100
	if maxJitterMs <= 0 {
		return baseMs
	}
	return baseMs + rand.IntN(maxJitterMs+1)
}

// publishRetry 发布重试消息
func (c *Client) publishRetry(
	delivery amqp.Delivery,
	currentCount int,
	processErr error,
) error {
	if processErr == nil {
		return fmt.Errorf(
			"rabbitmq retry error is nil",
		)
	}

	if currentCount >= c.config.MaxRetries {
		return fmt.Errorf(
			"retry count %d reached maximum %d",
			currentCount,
			c.config.MaxRetries,
		)
	}

	tier, err := selectRetryTier(
		c.config.RetryTiers,
		currentCount,
	)
	if err != nil {
		return err
	}

	nextCount := currentCount + 1

	delayMs := retryDelayMs(
		tier.DelayMs,
		c.config.RetryJitterPercent,
	)

	headers := copyHeaders(delivery.Headers)
	headers[retryCountHeader] = int32(nextCount)
	headers["x-last-error"] = processErr.Error()
	headers["x-last-failed-at"] = time.Now().
		UTC().
		Format(time.RFC3339Nano)

	// 首次缺少 MessageId 时，可以暂时保留空值，
	// 但正常 Publisher 应尽快设置稳定的 message_id。
	message := amqp.Publishing{
		Headers: headers,

		ContentType:     delivery.ContentType,
		ContentEncoding: delivery.ContentEncoding,

		// 重试消息必须持久化。
		DeliveryMode: amqp.Persistent,
		Priority:     delivery.Priority,

		CorrelationId: delivery.CorrelationId,
		ReplyTo:       delivery.ReplyTo,
		MessageId:     delivery.MessageId,
		Type:          delivery.Type,
		AppId:         delivery.AppId,

		// 保留原始业务消息发生时间。
		Timestamp: delivery.Timestamp,

		// RabbitMQ 的 per-message TTL，字符串单位为毫秒。
		Expiration: strconv.Itoa(delayMs),

		Body: delivery.Body,
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(
			c.config.PublishConfirmTimeoutMs,
		)*time.Millisecond,
	)
	defer cancel()

	// retryChannel 的 confirm 和 return 状态必须串行归属。
	c.retryMu.Lock()
	defer c.retryMu.Unlock()

	if err := publishConfirmed(
		ctx,
		c.retryChannel,
		c.retryReturns,
		c.config.RetryExchange,
		tier.RoutingKey,
		message,
	); err != nil {
		return fmt.Errorf(
			"rabbitmq publish retry tier=%d delay_ms=%d: %w",
			nextCount,
			delayMs,
			err,
		)
	}

	logger.Warn(
		"rabbitmq message scheduled for retry",
		"message_id", delivery.MessageId,
		"retry_count", nextCount,
		"retry_queue", tier.Queue,
		"routing_key", tier.RoutingKey,
		"delay_ms", delayMs,
	)

	return nil
}

// buildDLQPublishing 构造最终死信消息，并保留排障和重放所需的原始属性。
func buildDLQPublishing(
	delivery amqp.Delivery,
	currentCount int,
	kind FailureKind,
	processErr error,
	deadLetteredAt time.Time,
) (amqp.Publishing, error) {
	if currentCount < 0 {
		return amqp.Publishing{}, fmt.Errorf(
			"rabbitmq invalid negative retry count: %d",
			currentCount,
		)
	}
	if processErr == nil {
		return amqp.Publishing{}, fmt.Errorf("rabbitmq DLQ error is nil")
	}

	headers := copyHeaders(delivery.Headers)
	headers[retryCountHeader] = int32(currentCount)
	headers["x-final-error"] = processErr.Error()
	headers["x-failure-kind"] = kind.String()
	headers["x-dead-lettered-at"] = deadLetteredAt.UTC().Format(time.RFC3339Nano)
	headers["x-original-exchange"] = delivery.Exchange
	headers["x-original-routing-key"] = delivery.RoutingKey

	return amqp.Publishing{
		Headers:         headers,
		ContentType:     delivery.ContentType,
		ContentEncoding: delivery.ContentEncoding,
		DeliveryMode:    amqp.Persistent,
		Priority:        delivery.Priority,
		CorrelationId:   delivery.CorrelationId,
		ReplyTo:         delivery.ReplyTo,
		MessageId:       delivery.MessageId,
		Timestamp:       delivery.Timestamp,
		Type:            delivery.Type,
		AppId:           delivery.AppId,
		// 最终 DLQ 消息不能继承重试 TTL，必须持续保留直到人工处理。
		Expiration: "",
		Body:       delivery.Body,
	}, nil
}

// publishDLQ 可靠发布最终死信副本。调用方只能在返回 nil 后 ACK 原 delivery。
func (c *Client) publishDLQ(
	delivery amqp.Delivery,
	currentCount int,
	kind FailureKind,
	processErr error,
) error {
	if c == nil {
		return fmt.Errorf("rabbitmq client is nil")
	}

	message, err := buildDLQPublishing(
		delivery,
		currentCount,
		kind,
		processErr,
		time.Now(),
	)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(c.config.PublishConfirmTimeoutMs)*time.Millisecond,
	)
	defer cancel()

	// 重试和 DLQ 共用可靠发布 Channel，必须使用同一把锁串行关联 confirm/return。
	c.retryMu.Lock()
	defer c.retryMu.Unlock()

	if err := publishConfirmed(
		ctx,
		c.retryChannel,
		c.retryReturns,
		c.config.DeadLetterExchange,
		c.config.DeadLetterRoutingKey,
		message,
	); err != nil {
		return fmt.Errorf(
			"rabbitmq publish DLQ retry_count=%d failure_kind=%s: %w",
			currentCount,
			kind,
			err,
		)
	}

	logger.Error(
		"rabbitmq message published to DLQ",
		"message_id", delivery.MessageId,
		"retry_count", currentCount,
		"failure_kind", kind.String(),
		"err", processErr,
	)

	return nil
}
