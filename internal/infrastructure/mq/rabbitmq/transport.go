// Package rabbitmq 承载 RabbitMQ 的连接、发布、消费与确认细节。
//
// 本文件只处理 AMQP 传输层职责：连接和 Channel 生命周期、publisher confirm、
// mandatory return、consumer ACK 与 QoS，以及消费失败后的本地重试、延迟重试和最终 DLQ 路由。
// 未显式包装的业务错误默认按瞬时异常处理，确定性异常和系统性异常由 ProcessingError 标记。
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"GopherAI/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Config 描述 RabbitMQ 连接、拓扑和可靠投递参数。
type Config struct {
	// 连接参数由 bootstrap 从 config.toml 显式映射，Password 不得写入日志。
	Host     string
	Port     int
	Username string
	Password string
	Vhost    string

	// 主链路：Publisher -> MainExchange -> MainQueue -> Consumer。
	MainExchange   string
	MainQueue      string
	MainRoutingKey string

	// 重试链路：瞬时失败消息发布到 RetryExchange，再由 routing key 选择档位。
	// Consume 根据消息 Header 中的重试次数选择档位，超过上限或确定性失败时进入最终 DLQ。
	RetryExchange      string
	RetryTiers         []RetryTier
	RetryJitterPercent int
	MaxRetries         int
	LocalRetryDelaysMs []int

	// 最终失败链路：DeadLetterExchange -> DeadLetterQueue。
	DeadLetterExchange   string
	DeadLetterQueue      string
	DeadLetterRoutingKey string

	// PrefetchCount 限制 consumer 未 ACK 的在途消息数；ConfirmTimeout 限制发布确认等待时间。
	PrefetchCount           int
	PublishConfirmTimeoutMs int
}

// Client 复用一条 TCP Connection，并为发布、消费和重试分别持有 Channel。
type Client struct {
	// Connection 是实际 TCP 连接，创建成本较高，生命周期与应用一致。
	conn *amqp.Connection

	// delivery tag 和 publisher confirm 都是 Channel 级状态，因此三种职责不能混用 Channel。
	publishChannel *amqp.Channel
	consumeChannel *amqp.Channel
	// retryChannel 承担延迟重试与最终 DLQ 的 confirmed publish。
	retryChannel *amqp.Channel

	// 同一发布 Channel 上的 confirm 和 basic.return 必须和消息一一对应，因此串行发布。
	publishMu sync.Mutex
	// mandatory 消息无法路由时，Broker 通过此通道返回 basic.return。
	publishReturns <-chan amqp.Return

	retryMu      sync.Mutex
	retryReturns <-chan amqp.Return

	// 保存经过校验的配置，供发布、消费和后续重试逻辑使用。
	config Config
}

// Connect 建立连接、校验拓扑，并初始化各职责独立的长期 Channel。
func Connect(cfg Config) (*Client, error) {
	// AMQP 默认 vhost 是 "/"；在创建 URI 前统一空值，避免连接到错误的虚拟主机。
	if cfg.Vhost == "" {
		cfg.Vhost = "/"
	}
	// 在发起网络连接前校验完整拓扑，尽早暴露配置缺失和档位数量不一致。
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	// 使用客户端提供的 URI 类型生成连接串，可正确转义密码和 vhost 中的特殊字符。
	uri := amqp.URI{
		Scheme:   "amqp",
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.Username,
		Password: cfg.Password,
		Vhost:    cfg.Vhost,
	}

	logger.Info(
		"rabbitmq connecting",
		"host", cfg.Host,
		"port", cfg.Port,
		"vhost", cfg.Vhost,
	)

	conn, err := amqp.Dial(uri.String())
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial failed: %w", err)
	}

	// Connect 中任何中间步骤失败，都关闭 Connection；关闭 Connection 会连带回收已打开的 Channel。
	connected := false
	defer func() {
		if !connected {
			_ = conn.Close()
		}
	}()

	// 拓扑 Channel 是短生命周期管理通道，只负责幂等声明 exchange、queue 和 binding。
	// 若控制台中的同名对象参数不同，Broker 会返回 PRECONDITION_FAILED 并关闭该 Channel。
	topologyChannel, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq open topology channel: %w", err)
	}
	if err := declareTopology(topologyChannel, cfg); err != nil {
		_ = topologyChannel.Close()
		return nil, fmt.Errorf("rabbitmq declare topology: %w", err)
	}
	if err := topologyChannel.Close(); err != nil {
		return nil, fmt.Errorf("rabbitmq close topology channel: %w", err)
	}

	// 正常业务消息使用独立发布 Channel，并开启 confirm 模式。
	// Confirm(false) 中的 false 表示等待 Broker 对 confirm.select 作出响应。
	publishChannel, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq open publish channel: %w", err)
	}
	if err := publishChannel.Confirm(false); err != nil {
		return nil, fmt.Errorf("rabbitmq enable publish confirm: %w", err)
	}
	// 缓冲大小为 1，因为 publishMu 保证任意时刻最多只有一条待确认的正常发布。
	publishReturns := publishChannel.NotifyReturn(make(chan amqp.Return, 1))

	// 消费 Channel 独立维护 delivery tag；ACK/NACK 必须在收到 delivery 的同一 Channel 上发送。
	consumeChannel, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq open consume channel: %w", err)
	}
	// prefetchSize=0 表示不按字节限流；global=false 将窗口作用于当前 consumer。
	// 该窗口只控制未确认消息数量，不等于应用层批处理大小。
	if err := consumeChannel.Qos(cfg.PrefetchCount, 0, false); err != nil {
		return nil, fmt.Errorf("rabbitmq configure consumer qos: %w", err)
	}

	// 重试和最终 DLQ 的“先可靠发布、再 ACK 原消息”需要独立 Channel 与 confirm 状态。
	// publishRetry、publishDLQ 和 Consume 失败状态机共同使用该 Channel。
	retryChannel, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq open retry channel: %w", err)
	}
	if err := retryChannel.Confirm(false); err != nil {
		return nil, fmt.Errorf("rabbitmq enable retry confirm: %w", err)
	}
	retryReturns := retryChannel.NotifyReturn(make(chan amqp.Return, 1))

	client := &Client{
		conn:           conn,
		publishChannel: publishChannel,
		publishReturns: publishReturns,
		consumeChannel: consumeChannel,
		retryChannel:   retryChannel,
		retryReturns:   retryReturns,
		config:         cfg,
	}
	connected = true

	logger.Info(
		"rabbitmq connected",
		"host", cfg.Host,
		"port", cfg.Port,
		"vhost", cfg.Vhost,
		"main_queue", cfg.MainQueue,
	)
	return client, nil
}

// Publish 向主交换机持久化发布，并等待 Broker confirm。
// confirm 只表示 Broker 接管了消息，不表示 Consumer 已处理，更不表示数据库事务已提交。
func (c *Client) Publish(msgID string, body []byte) error {
	if c == nil || c.publishChannel == nil {
		return fmt.Errorf("rabbitmq publish channel is not initialized")
	}

	// Channel 的 confirm/return 状态需要串行归属，避免并发发布时对应错消息。
	// 这里选择正确性优先的逐条发布确认；吞吐不足时再升级为序号映射的异步 confirms。
	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	// Confirm 超时属于“结果未知”：Broker 可能已经接收，只是确认没有及时到达。
	// 调用方重试可能产生重复消息，所以消费端必须依赖 message_id 保证幂等。
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(c.config.PublishConfirmTimeoutMs)*time.Millisecond,
	)
	defer cancel()

	err := publishConfirmed(
		ctx,
		c.publishChannel,
		c.publishReturns,
		// 显式主交换机和 routing key，取代旧实现中的默认交换机直投队列。
		c.config.MainExchange,
		c.config.MainRoutingKey,
		amqp.Publishing{
			// 添加 retry count header，用于当前是第几次重试。
			Headers: amqp.Table{
				retryCountHeader: int32(0),
			},
			// Persistent 需要和 durable exchange/queue 配合，才能在 Broker 重启后保留拓扑信息。
			DeliveryMode: amqp.Persistent,
			MessageId:    msgID,
			ContentType:  "application/json",
			Timestamp:    time.Now().UTC(),
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("rabbitmq publish main message: %w", err)
	}

	return nil
}

// publishConfirmed 使用 mandatory 和 publisher confirm 可靠发布一条消息。
// 调用方必须按 Channel 持有互斥锁，保证 confirm 和 basic.return 与当前消息一一对应。
func publishConfirmed(
	ctx context.Context,
	channel *amqp.Channel,
	returns <-chan amqp.Return,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	if ctx == nil {
		return fmt.Errorf("rabbitmq publish context is nil")
	}
	if channel == nil || channel.IsClosed() {
		return fmt.Errorf("rabbitmq publish channel is not available")
	}
	if err := discardStaleReturns(returns); err != nil {
		return err
	}

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		routingKey,
		// 无法路由到队列时要求 Broker 发送 basic.return。
		true,
		// RabbitMQ 不支持 immediate，固定为 false。
		false,
		message,
	)
	if err != nil {
		return fmt.Errorf("rabbitmq publish failed: %w", err)
	}
	if confirmation == nil {
		return fmt.Errorf("rabbitmq publisher confirm is not enabled")
	}

	// ACK 表示 Broker 接管了消息；NACK 或超时都不能视为可靠发布成功。
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("rabbitmq publish confirm failed: %w", err)
	}
	if !acked {
		return fmt.Errorf("rabbitmq publish was negatively acknowledged")
	}

	// 对 mandatory 消息，RabbitMQ 会先发送 basic.return，再发送 publisher confirm。
	select {
	case returned, ok := <-returns:
		if !ok {
			return fmt.Errorf("rabbitmq publish return listener is closed")
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

// Consume 启动主队列消费，并按错误分类执行本地重试、延迟重试、最终 DLQ 或停止消费。
//
// 首次瞬时失败按配置进行本地快速重试；仍失败时按 x-retry-count 进入下一档延迟队列。
// 确定性异常或重试耗尽时可靠发布到最终 DLQ，retry/DLQ confirm 成功后才 ACK 原消息；
// 系统性异常或可靠发布失败时不 ACK，并通过关闭消费 Channel 让原消息重新入队。
func (c *Client) Consume(handle func(body []byte) error) {
	msgs, err := c.consumeChannel.Consume(
		c.config.MainQueue,
		// consumer tag 留空，由客户端自动生成。
		"",
		// autoAck=false：业务处理完成前消息保持 unacked，连接中断后可重新投递。
		false,
		// exclusive=false：允许同一队列扩展多个消费者实例。
		false,
		// noLocal=false；RabbitMQ 不支持 AMQP 0-9-1 的 no-local 语义。
		false,
		// noWait=false：等待 Broker 确认消费注册成功。
		false,
		nil,
	)
	if err != nil {
		logger.Error("rabbitmq consume failed", "queue", c.config.MainQueue, "err", err)
		return
	}
	defer func() {
		if c.consumeChannel == nil || c.consumeChannel.IsClosed() {
			return
		}
		if err := c.consumeChannel.Close(); err != nil {
			logger.Error("close consume channel failed", "err", err)
		}
	}()

	for msg := range msgs {
		currentCount, countErr := retryCount(msg.Headers)

		var processErr error

		switch {
		case countErr != nil:
			// 重试 Header 已损坏，属于确定性坏消息。
			processErr = permanentError(fmt.Errorf(
				"rabbitmq invalid retry count header: %w",
				countErr,
			))

		case currentCount > c.config.MaxRetries:
			// 合法链路中最大只可能等于 MaxRetries。
			processErr = permanentError(fmt.Errorf(
				"rabbitmq retry count %d exceeds maximum %d",
				currentCount,
				c.config.MaxRetries,
			))

		case currentCount == 0:
			// 只有首次消费执行 100ms、500ms 本地快速重试。
			processErr = c.handleWithLocalRetry(func() error {
				return handle(msg.Body)
			})

		default:
			// 从延迟重试队列返回的消息只执行一次业务操作。
			processErr = handle(msg.Body)
		}

		if processErr != nil {
			kind := classifyError(processErr)

			var publishErr error

			switch kind {
			case FailurePermanent:
				// 确定性异常直接进入最终 DLQ。
				publishErr = c.publishDLQ(
					msg,
					currentCount,
					kind,
					processErr,
				)

			case FailureTransient:
				if currentCount >= c.config.MaxRetries {
					// 已经完成五次延迟重试，最终进入 DLQ。
					publishErr = c.publishDLQ(
						msg,
						currentCount,
						kind,
						processErr,
					)
				} else {
					// 进入下一档延迟重试队列。
					publishErr = c.publishRetry(
						msg,
						currentCount,
						processErr,
					)
				}

			case FailureAbort:
				// 系统性异常：不 ACK、不进 DLQ，停止消费者。
				logger.Error(
					"rabbitmq consumer aborted",
					"message_id", msg.MessageId,
					"retry_count", currentCount,
					"err", processErr,
				)
				return

			default:
				// 未知分类采取最保守策略：停止消费者。
				logger.Error(
					"rabbitmq unknown failure kind",
					"message_id", msg.MessageId,
					"failure_kind", kind,
					"err", processErr,
				)
				return
			}

			if publishErr != nil {
				// retry/DLQ 未可靠发布，不能 ACK 原消息。
				// return 后 defer 关闭 Channel，原消息重新入队。
				logger.Error(
					"rabbitmq failure message publish failed",
					"message_id", msg.MessageId,
					"retry_count", currentCount,
					"failure_kind", kind.String(),
					"err", publishErr,
				)
				return
			}
		}

		// 到达这里有三种情况：
		// 1. 业务处理成功；
		// 2. 重试副本 confirmed publish 成功；
		// 3. DLQ 副本 confirmed publish 成功。
		if err := msg.Ack(false); err != nil {
			logger.Error(
				"rabbitmq ack failed",
				"message_id", msg.MessageId,
				"err", err,
			)
			return
		}
	}
}

// handleWithLocalRetry 本地快速重试 handle 包装器，接收闭包，闭包内实现业务逻辑，返回错误
func (c *Client) handleWithLocalRetry(operation func() error) error {
	err := operation()
	if err == nil || classifyError(err) == FailurePermanent || classifyError(err) == FailureAbort {
		return err
	}

	for _, delayMs := range c.config.LocalRetryDelaysMs {
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
		err = operation()
		if err == nil || classifyError(err) == FailurePermanent || classifyError(err) == FailureAbort {
			return err
		}
	}

	return err
}

// discardStaleReturns 丢弃上一次超时发布可能晚到的 return，防止污染下一次发布判断。
// 清理不代表上一条消息一定失败；发布超时仍然属于结果未知，需要依靠消费幂等兜底。
func discardStaleReturns(returns <-chan amqp.Return) error {
	if returns == nil {
		return fmt.Errorf("rabbitmq publish return listener is not initialized")
	}

	for {
		select {
		case _, ok := <-returns:
			if !ok {
				return fmt.Errorf("rabbitmq publish return listener is closed")
			}
			logger.Warn("rabbitmq discarded stale publish return")
		default:
			return nil
		}
	}
}

// validateConfig 在联网前验证拓扑完整性和策略约束，避免用部分配置启动应用。
func validateConfig(cfg Config) error {
	if cfg.Host == "" {
		return fmt.Errorf("rabbitmq host is empty")
	}
	if cfg.Port <= 0 {
		return fmt.Errorf("rabbitmq port must be greater than zero")
	}
	if cfg.Username == "" {
		return fmt.Errorf("rabbitmq username is empty")
	}
	if cfg.MainExchange == "" || cfg.MainQueue == "" || cfg.MainRoutingKey == "" {
		return fmt.Errorf("rabbitmq main topology is incomplete")
	}
	if cfg.RetryExchange == "" {
		return fmt.Errorf("rabbitmq retry exchange is empty")
	}
	if cfg.DeadLetterExchange == "" || cfg.DeadLetterQueue == "" || cfg.DeadLetterRoutingKey == "" {
		return fmt.Errorf("rabbitmq dead-letter topology is incomplete")
	}
	if cfg.MaxRetries <= 0 || cfg.MaxRetries != len(cfg.RetryTiers) {
		return fmt.Errorf(
			"rabbitmq max retries %d does not match retry tier count %d",
			cfg.MaxRetries,
			len(cfg.RetryTiers),
		)
	}
	for i, delay := range cfg.LocalRetryDelaysMs {
		if delay <= 0 {
			return fmt.Errorf("rabbitmq local retry delay %d must be greater than zero", i+1)
		}
	}
	if cfg.RetryJitterPercent < 0 || cfg.RetryJitterPercent > 25 {
		return fmt.Errorf("rabbitmq retry jitter percent must be between 0 and 25")
	}
	if cfg.PrefetchCount <= 0 {
		return fmt.Errorf("rabbitmq prefetch count must be greater than zero")
	}
	if cfg.PublishConfirmTimeoutMs <= 0 {
		return fmt.Errorf("rabbitmq publish confirm timeout must be greater than zero")
	}

	for i, tier := range cfg.RetryTiers {
		if tier.Queue == "" || tier.RoutingKey == "" {
			return fmt.Errorf("rabbitmq retry tier %d topology is incomplete", i+1)
		}
		if tier.DelayMs <= 0 {
			return fmt.Errorf("rabbitmq retry tier %d delay must be greater than zero", i+1)
		}
	}
	return nil
}

// Close 按 retry、consume、publish Channel 到 TCP Connection 的顺序释放资源。
// errors.Join 保留全部关闭错误，避免第一个失败阻止后续资源释放。
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	var errs []error
	channels := []struct {
		name string
		ch   *amqp.Channel
	}{
		{name: "retry", ch: c.retryChannel},
		{name: "consume", ch: c.consumeChannel},
		{name: "publish", ch: c.publishChannel},
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
