package rabbitmq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"GopherAI/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// buildGroupDeadLetterPublishing 保留原消息和重试元数据，并补充最终失败信息。
func buildGroupDeadLetterPublishing(
	consumerGroup string,
	delivery amqp.Delivery,
	kind FailureKind,
	cause error,
	deadLetteredAt time.Time,
) (amqp.Publishing, error) {
	consumerGroup = strings.TrimSpace(consumerGroup)
	if consumerGroup == "" {
		return amqp.Publishing{}, errors.New("build group dead letter: consumer group is empty")
	}
	if cause == nil {
		return amqp.Publishing{}, errors.New("build group dead letter: cause is nil")
	}
	if deadLetteredAt.IsZero() {
		return amqp.Publishing{}, errors.New("build group dead letter: dead letter time is zero")
	}

	headers := copyHeaders(delivery.Headers)
	headers["x-consumer-group"] = consumerGroup
	headers["x-final-error"] = cause.Error()
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
		// DLQ 消息不能继承延迟消息的 TTL，否则会在排障前过期。
		Expiration: "",
		Body:       bytes.Clone(delivery.Body),
	}, nil
}

// publishGroupDeadLetter 可靠发布到指定消费者组 DLQ；返回 nil 后调用方才可 ACK 原消息。
func (c *Client) publishGroupDeadLetter(
	ctx context.Context,
	consumerGroup string,
	exchange string,
	routingKey string,
	delivery amqp.Delivery,
	kind FailureKind,
	cause error,
) error {
	if c == nil {
		return errors.New("publish group dead letter: client is nil")
	}
	if ctx == nil {
		return errors.New("publish group dead letter: context is nil")
	}

	exchange = strings.TrimSpace(exchange)
	routingKey = strings.TrimSpace(routingKey)
	if exchange == "" {
		return errors.New("publish group dead letter: exchange is empty")
	}
	if routingKey == "" {
		return errors.New("publish group dead letter: routing key is empty")
	}

	message, err := buildGroupDeadLetterPublishing(
		consumerGroup,
		delivery,
		kind,
		cause,
		time.Now(),
	)
	if err != nil {
		return err
	}

	// retryChannel 的 confirm 和 mandatory return 必须串行归属当前消息。
	c.retryMu.Lock()
	defer c.retryMu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish group dead letter cancelled before publish: %w", err)
	}
	if err := publishConfirmed(
		ctx,
		c.retryChannel,
		c.retryReturns,
		exchange,
		routingKey,
		message,
	); err != nil {
		return fmt.Errorf("publish consumer group %q dead letter: %w", consumerGroup, err)
	}

	logger.Error(
		"group delivery published to DLQ",
		"consumerGroup", consumerGroup,
		"messageID", delivery.MessageId,
		"failureKind", kind.String(),
		"err", cause,
	)
	return nil
}
