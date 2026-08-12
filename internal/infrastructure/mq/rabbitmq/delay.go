package rabbitmq

import (
	taskDomain "GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Task 在 MQ 中的传输格式
type delayEnvelope struct {
	TaskID    string `json:"task_id"`
	AccountNo string `json:"account_no"`
	Status    uint8  `json:"status"`
	// Message 相关字段
	MessageID   string            `json:"message_id"`
	Topic       string            `json:"topic"`
	Headers     map[string]string `json:"headers"`
	Body        []byte            `json:"body"`
	TimestampMs int64             `json:"timestamp_ms"`

	TargetKind    uint8  `json:"target_kind"`
	ConsumerGroup string `json:"consumer_group"`
	RetryAttempt  uint32 `json:"retry_attempt"`
	TargetAtMs    int64  `json:"target_at_ms"`
	Version       int64  `json:"version"`
}

// Task 解编码，domain<->envelope
func encodeTask(task taskDomain.Task) (delayEnvelope, error) {
	if err := task.Validate(); err != nil {
		return delayEnvelope{}, err
	}
	return delayEnvelope{
		TaskID:        task.ID,
		AccountNo:     task.AccountNo,
		Status:        uint8(task.Status),
		MessageID:     task.Message.ID,
		Topic:         task.Message.Topic,
		Headers:       task.Message.Headers,
		Body:          task.Message.Body,
		TimestampMs:   task.Message.Timestamp.UnixMilli(),
		TargetKind:    uint8(task.Target.Kind),
		ConsumerGroup: task.Target.ConsumerGroup,
		RetryAttempt:  task.RetryAttempt,
		TargetAtMs:    task.TargetAt,
		Version:       task.Version,
	}, nil
}

func decodeTask(envelope delayEnvelope) (taskDomain.Task, error) {
	return taskDomain.Task{
		ID:        envelope.TaskID,
		AccountNo: envelope.AccountNo,
		Status:    taskDomain.Status(envelope.Status),
		Message: messageDomain.Message{ID: envelope.MessageID,
			Topic:     envelope.Topic,
			Headers:   envelope.Headers,
			Body:      envelope.Body,
			Timestamp: time.UnixMilli(envelope.TimestampMs).UTC()},
		Target: messageDomain.Target{
			Kind:          messageDomain.TargetKind(envelope.TargetKind),
			ConsumerGroup: envelope.ConsumerGroup},
		RetryAttempt: envelope.RetryAttempt,
		TargetAt:     envelope.TargetAtMs,
		Version:      envelope.Version,
	}, nil
}

// Delay RabbitMQ 配置与拓扑
type DelayConfig struct {
	// LevelExchange 接收 Level 1～MaxLevel 的延迟任务，
	// routing key 决定消息进入哪个固定 TTL Queue。
	LevelExchange string

	// LevelQueuePrefix 用于生成 Level Queue 名称，
	// 例如 gopherai.delay.level.1、gopherai.delay.level.60。
	LevelQueuePrefix string

	// LevelRoutingPrefix 用于生成 Level routing key，
	// 例如 delay.level.1、delay.level.60。
	LevelRoutingPrefix string

	// DispatcherExchange 是所有 Level Queue 的 DLX，
	// Level 0 任务也会直接发布到该 Exchange。
	DispatcherExchange string

	// DispatcherQueue 是 Dispatcher 消费的持久化 Inbox。
	// 消息进入该 Queue 后，在 FinalPublisher confirm 前保持未 ACK。
	DispatcherQueue string

	// DispatcherRoutingKey 用于把 Level 到期消息和 Level 0
	// 消息精确路由到 DispatcherQueue。
	DispatcherRoutingKey string

	// MaxLevel 是允许声明和发布的最大整秒 Level，
	// 当前固定为 60，对应最长 60 秒 RabbitMQ 延迟。
	MaxLevel int

	// ConfirmTimeout 限制 LevelPublisher 等待 Broker confirm
	// 的最长时间；超时表示发布结果未知。
	ConfirmTimeout time.Duration
}

func validateDelayConfig(config DelayConfig) error {
	switch {
	case config.LevelExchange == "":
		return errors.New("delay level exchange is empty")
	case config.LevelQueuePrefix == "":
		return errors.New("delay level queue prefix is empty")
	case config.LevelRoutingPrefix == "":
		return errors.New("delay level routing prefix is empty")
	case config.DispatcherExchange == "":
		return errors.New("delay dispatcher exchange is empty")
	case config.DispatcherQueue == "":
		return errors.New("delay dispatcher queue is empty")
	case config.DispatcherRoutingKey == "":
		return errors.New("delay dispatcher routing key is empty")
	case config.MaxLevel < 1 || config.MaxLevel > 60:
		return fmt.Errorf(
			"delay max level %d is outside 1..60",
			config.MaxLevel,
		)
	case config.ConfirmTimeout <= 0:
		return errors.New("delay confirm timeout must be positive")
	default:
		return nil
	}
}

func levelQueueName(config DelayConfig, level int) string {
	return fmt.Sprintf("%s.%d", config.LevelQueuePrefix, level)
}

func levelRoutingKey(config DelayConfig, level int) string {
	return fmt.Sprintf("%s.%d", config.LevelRoutingPrefix, level)
}

// 声明 Exchange
func declareLevelExchange(ch *amqp.Channel, config DelayConfig) error {
	if err := validateDelayConfig(config); err != nil {
		return err
	}
	for _, exchange := range []string{
		config.LevelExchange,
		config.DispatcherExchange,
	} {
		if err := ch.ExchangeDeclare(
			exchange,
			"dirict",
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			return fmt.Errorf(
				"declare level exchange %s failed: %w",
				exchange,
				err,
			)
		}
	}
	return nil
}

// 声明 Dispatcher Queue
func declareLevelQueue(ch *amqp.Channel, config DelayConfig) error {
	if err := validateDelayConfig(config); err != nil {
		return err
	}
	for level := 1; level <= config.MaxLevel; level++ {
		queue := levelQueueName(config, level)
		routingKey := levelRoutingKey(config, level)
		if _, err := ch.QueueDeclare(
			queue,
			true,
			false,
			false,
			false,
			amqp.Table{
				"x-queue-type":              "quorum",
				"x-message-ttl":             int32(level * 1000),
				"x-dead-letter-exchange":    config.DispatcherExchange,
				"x-dead-letter-routing-key": config.DispatcherRoutingKey,
			},
		); err != nil {
			return fmt.Errorf(
				"declare level queue %s failed: %w",
				queue,
				err,
			)
		}
		if err := ch.QueueBind(
			queue,
			routingKey,
			config.LevelExchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf(
				"bind delay level queue %q: %w", queue, err,
			)
		}
	}
	return nil
}

// 声明dispatcher inbox
func declareDispatcherInbox(ch *amqp.Channel, config DelayConfig) error {
	if err := validateDelayConfig(config); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(
		config.DispatcherQueue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-queue-type": "quorum",
		},
	); err != nil {
		return fmt.Errorf(
			"declare dispatcher queue %q: %w",
			config.DispatcherQueue,
			err,
		)
	}
	if err := ch.QueueBind(
		config.DispatcherQueue,
		config.DispatcherRoutingKey,
		config.DispatcherExchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf(
			"bind dispatcher queue %q: %w",
			config.DispatcherQueue,
			err,
		)
	}
	return nil
}

// Level Publisher 将 MySQL 的长延迟任务转交给 Level MQ
