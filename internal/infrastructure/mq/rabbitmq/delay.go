package rabbitmq

import (
	taskDomain "GopherAI/internal/domain/delay"
	messageDomain "GopherAI/internal/domain/message"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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
			"direct",
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

// 声明 dispatcher inbox
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

func declareDelayTopology(ch *amqp.Channel, config DelayConfig) error {
	if err := declareLevelExchange(ch, config); err != nil {
		return err
	}
	if err := declareDispatcherInbox(ch, config); err != nil {
		return err
	}
	if err := declareLevelQueue(ch, config); err != nil {
		return err
	}
	return nil
}

// Level Publisher 将 MySQL 的长延迟任务转交给 Level MQ
type LevelPublisher struct {
	// 使用独立channel，避免与业务channel竞争锁
	channel *amqp.Channel
	returns <-chan amqp.Return
	mu      sync.Mutex
	config  DelayConfig
}

var _ taskDomain.LevelPublisher = (*LevelPublisher)(nil)

func NewLevelPublisher(client *Client, config DelayConfig) (*LevelPublisher, error) {
	if client == nil || client.conn == nil {
		return nil, errors.New("new level publisher: rabbitmq client is nil")
	}

	if err := validateDelayConfig(config); err != nil {
		return nil, err
	}

	// 用于声明 Delay 拓扑的临时channel
	topologyChannel, err := client.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open delay topology channel: %w", err)
	}

	if err := declareDelayTopology(topologyChannel, config); err != nil {
		_ = topologyChannel.Close()
		return nil, fmt.Errorf("declare delay topology: %w", err)
	}
	if err := topologyChannel.Close(); err != nil {
		return nil, fmt.Errorf(
			"close delay topology channel: %w",
			err,
		)
	}

	publishChannel, err := client.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open delay publish channel: %w", err)
	}

	// 启动发布确认，每条发布消息都收到 Broker 的 ACK/NACK。false（noWait参数）表示等待 confirm.select-ok 生效，不使用 no-wait
	// 确保在开始发布前 Channel 已经进入 confirm 模式
	if err := publishChannel.Confirm(false); err != nil {
		_ = publishChannel.Close()
		return nil, fmt.Errorf(
			"enable level publisher confirm: %w",
			err,
		)
	}
	// 监听 mandatory 消息无法路由时，Broker 通过此通道返回 basic.return，此通道是公共的。
	// 缓冲为1是因为当前 LevelPublisher 使用 mutex 串行发布，任意时刻最多处理一条 confirmed message。
	returns := publishChannel.NotifyReturn(make(chan amqp.Return, 1))

	return &LevelPublisher{
		channel: publishChannel,
		returns: returns,
		config:  config,
	}, nil
}

// Publish 将延迟任务可靠转交给指定 Level；Level 0 会绕过 TTL Queue，直接进入 Dispatcher Inbox。
//
// 返回值同时表达消息所有权是否已经转移：
//   - nil：Broker 已 ACK，且 mandatory publish 未被退回，Poller 可以调用 MarkLevelQueued；
//   - PublishRejectedError：发布前校验失败、Broker NACK 或 mandatory return，确认下游未接管，
//     Poller 可以安全调用 Release 释放 MySQL 租约；
//   - 其他 error：发送或 confirm 结果未知，消息可能已经进入 Broker，Poller 必须保留租约等待恢复，
//     不能立即 Release，否则可能造成重复的并发所有者。
//
// 同一个 AMQP Channel 的 deferred confirm 和 basic.return 必须与当前消息一一对应，
// 因此本方法使用互斥锁串行发布，并把编码、路由等发布前错误与发布后的不确定错误严格分开。
//
// TODO：如果后续证明这里的单channel是吞吐瓶颈，就使用channel pool优化
func (p *LevelPublisher) Publish(ctx context.Context, level int, task taskDomain.Task) error {
	// 以下校验都发生在调用 AMQP Publish 之前，失败时可以确认 Broker 尚未接管消息。
	if p == nil {
		return taskDomain.NewPublishRejectedError(
			errors.New("level publisher is nil"))
	}
	if ctx == nil {
		return taskDomain.NewPublishRejectedError(
			errors.New("context is nil"))
	}
	if level < 0 || level > p.config.MaxLevel {
		return taskDomain.NewPublishRejectedError(fmt.Errorf(
			"invalid level %d: must be in 0..%d", level, p.config.MaxLevel))
	}

	// 领域校验与 Envelope 编码失败时消息尚未发送，属于明确未接管。
	envelope, err := encodeTask(task)
	if err != nil {
		return taskDomain.NewPublishRejectedError(fmt.Errorf(
			"encode delay task %q: %w",
			task.ID,
			err))
	}

	// JSON 序列化同样发生在发布前，失败时 Poller 可以安全释放租约。
	body, err := json.Marshal(envelope)
	if err != nil {
		return taskDomain.NewPublishRejectedError(fmt.Errorf(
			"marshal delay task %q: %w",
			task.ID,
			err))
	}

	// 延迟 Envelope 使用 schedule ID 作为 AMQP message_id；业务 message_id 保留在 Envelope 内。
	// 消息不设置 per-message TTL，实际延迟完全由对应 Level Queue 的固定 TTL 决定。
	message := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    envelope.TaskID,
		Type:         "goai.delay.task.v1",
		Timestamp:    time.Now().UTC(),
		Body:         body,
	}

	// 同一个 Channel 的 confirm 和 mandatory return 必须串行关联。因为 basic.return 是公共的，多个 goroutine 都可以从同一个 p.returns 中读取
	p.mu.Lock()
	defer p.mu.Unlock()

	// 等锁期间调用方可能已经取消，此时尚未发布，可以安全视为明确未接管。
	if err := ctx.Err(); err != nil {
		return taskDomain.NewPublishRejectedError(fmt.Errorf(
			"publish delay task %q cancelled before publish: %w",
			task.ID,
			err,
		))
	}

	if p.channel == nil || p.channel.IsClosed() {
		// 尚未调用 Publish，Channel 不可用仍属于明确未接管。
		return taskDomain.NewPublishRejectedError(
			errors.New("delay publish channel is unavailable"),
		)
	}
	if p.returns == nil {
		// 缺少 return 监听器时无法执行 mandatory 发布，但此时消息尚未发送。
		return taskDomain.NewPublishRejectedError(
			errors.New("delay return listener is unavailable"),
		)
	}

	// 清理上一条结果未知消息可能迟到的 basic.return。
	if err := discardStaleReturns(p.returns); err != nil {
		return taskDomain.NewPublishRejectedError(fmt.Errorf(
			"prepare delay publish return listener: %w",
			err,
		))
	}

	// ConfirmTimeout 只限制当前消息等待 Broker confirm 的时间；超时不等于 Broker 未收到。
	publishCtx, cancel := context.WithTimeout(
		ctx,
		p.config.ConfirmTimeout,
	)
	defer cancel()

	// 在发布前计算 level
	level = adjustLevelAfterWaitLock(level, task.TargetAt, time.Now().UnixMilli())

	exchange := p.config.LevelExchange
	routingKey := levelRoutingKey(p.config, level)

	// Level 0 没有对应的 TTL Queue，已经到期或不足一秒的任务直接进入 Dispatcher Inbox。
	if level == 0 {
		exchange = p.config.DispatcherExchange
		routingKey = p.config.DispatcherRoutingKey
	}

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		publishCtx,
		exchange,
		routingKey,
		true,  // mandatory
		false, // immediate
		message,
	)
	if err != nil {
		// amqp091-go 明确说明：Publish 返回错误不代表 Broker 一定没收到。
		// 因此这里必须保留为“结果未知”，不能包装 PublishRejectedError。
		return fmt.Errorf(
			"publish delay task %q to level %d has unknown outcome: %w",
			task.ID,
			level,
			err,
		)
	}
	if confirmation == nil {
		// 消息可能已经发出，只是 Channel 没有正确进入 confirm 模式。
		return fmt.Errorf(
			"publish delay task %q has unknown outcome: publisher confirm is unavailable",
			task.ID,
		)
	}

	acked, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		// 超时或连接中断时，消息可能已经被 Broker 接收。
		return fmt.Errorf(
			"wait delay task %q confirm has unknown outcome: %w",
			task.ID,
			err,
		)
	}
	if !acked {
		// Broker 明确 NACK，说明 Level MQ 没有接管，可以安全释放 MySQL 租约。
		return taskDomain.NewPublishRejectedError(fmt.Errorf(
			"delay task %q was negatively acknowledged",
			task.ID,
		))
	}

	// 对 mandatory publish，RabbitMQ 会先发送 basic.return，再发送 publisher confirm。
	// 收到 return 表示 Broker 接收了发布请求但消息没有路由到目标 Queue，下游明确未接管。
	select {
	case returned, ok := <-p.returns:
		if !ok {
			// ACK 后监听器异常关闭时无法确认是否遗漏 return，按结果未知处理。
			return fmt.Errorf(
				"publish delay task %q has unknown outcome: return listener closed",
				task.ID,
			)
		}
		return taskDomain.NewPublishRejectedError(fmt.Errorf(
			"delay task %q was returned: code=%d text=%s exchange=%s routing_key=%s",
			task.ID,
			returned.ReplyCode,
			returned.ReplyText,
			returned.Exchange,
			returned.RoutingKey,
		))
	default:
		// ACK 且没有 mandatory return，Level MQ 已可靠接管消息，所有权转移完成。
		return nil
	}
}

func (p *LevelPublisher) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel == nil {
		return nil
	}
	if err := p.channel.Close(); err != nil {
		return fmt.Errorf("close delay publish channel: %w", err)
	}
	p.channel = nil
	return nil
}

// adjustLevelAfterWaitLock 在获取锁之后修正 level，消除等待期间过长导致 level 变更
func adjustLevelAfterWaitLock(oldLevel int, targetAtMs int64, nowMs int64) int {
	if oldLevel == 0 {
		return 0
	}

	newMs := targetAtMs - nowMs
	if newMs <= 0 {
		return 0
	}

	level := int(newMs / 1000)
	if level > oldLevel {
		return oldLevel
	}
	return level
}
