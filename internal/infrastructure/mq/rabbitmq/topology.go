package rabbitmq

import (
	"errors"
	"fmt"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DelayConsumerGroupConfig 描述一个消费者组的业务队列、订阅和死信拓扑。
type DelayConsumerGroupConfig struct {
	// Name 同时作为 Redrive Exchange 精确回投该组的 routing key。
	Name string
	// Queue 由同组的 Consumer 实例竞争消费。
	Queue string
	// Topics 是该组在 Topic Exchange 上订阅的 routing key。
	Topics []string
	// DeadLetterQueue 保存该组永久失败或重试耗尽的消息。
	DeadLetterQueue string
	// DeadLetterRoutingKey 将失败消息精确路由到该组 DLQ。
	DeadLetterRoutingKey string
}

// DelayConfig 描述统一延迟链路的 RabbitMQ 拓扑和发布确认参数。
type DelayConfig struct {
	// LevelExchange 接收 Level 1～MaxLevel 的延迟任务。
	LevelExchange      string
	LevelQueuePrefix   string
	LevelRoutingPrefix string

	// DispatcherExchange 接收 Level 到期任务和 Level 0 任务。
	DispatcherExchange   string
	DispatcherQueue      string
	DispatcherRoutingKey string

	// TopicExchange 负责正常广播，RedriveExchange 负责消费者组精确回投。
	TopicExchange      string
	RedriveExchange    string
	DeadLetterExchange string
	ConsumerGroups     []DelayConsumerGroupConfig

	// MaxLevel 当前限制在 1～60，对应固定 TTL Level Queue。
	MaxLevel int
	// ConfirmTimeout 限制 LevelPublisher 等待 Broker confirm 的时间。
	ConfirmTimeout time.Duration
}

func validateDelayConfig(config DelayConfig) error {
	switch {
	case strings.TrimSpace(config.LevelExchange) == "":
		return errors.New("delay level exchange is empty")
	case strings.TrimSpace(config.LevelQueuePrefix) == "":
		return errors.New("delay level queue prefix is empty")
	case strings.TrimSpace(config.LevelRoutingPrefix) == "":
		return errors.New("delay level routing prefix is empty")
	case strings.TrimSpace(config.DispatcherExchange) == "":
		return errors.New("delay dispatcher exchange is empty")
	case strings.TrimSpace(config.DispatcherQueue) == "":
		return errors.New("delay dispatcher queue is empty")
	case strings.TrimSpace(config.DispatcherRoutingKey) == "":
		return errors.New("delay dispatcher routing key is empty")
	case strings.TrimSpace(config.TopicExchange) == "":
		return errors.New("delay topic exchange is empty")
	case strings.TrimSpace(config.RedriveExchange) == "":
		return errors.New("delay redrive exchange is empty")
	case strings.TrimSpace(config.DeadLetterExchange) == "":
		return errors.New("delay dead letter exchange is empty")
	case len(config.ConsumerGroups) == 0:
		return errors.New("delay consumer groups are empty")
	case config.MaxLevel < 1 || config.MaxLevel > 60:
		return fmt.Errorf("delay max level %d is outside 1..60", config.MaxLevel)
	case config.ConfirmTimeout <= 0:
		return errors.New("delay confirm timeout must be positive")
	}

	for index, group := range config.ConsumerGroups {
		switch {
		case strings.TrimSpace(group.Name) == "":
			return fmt.Errorf("delay consumer group %d name is empty", index)
		case strings.TrimSpace(group.Queue) == "":
			return fmt.Errorf("delay consumer group %q queue is empty", group.Name)
		case len(group.Topics) == 0:
			return fmt.Errorf("delay consumer group %q topics are empty", group.Name)
		case strings.TrimSpace(group.DeadLetterQueue) == "":
			return fmt.Errorf("delay consumer group %q dead letter queue is empty", group.Name)
		case strings.TrimSpace(group.DeadLetterRoutingKey) == "":
			return fmt.Errorf("delay consumer group %q dead letter routing key is empty", group.Name)
		}

		for topicIndex, topic := range group.Topics {
			if strings.TrimSpace(topic) == "" {
				return fmt.Errorf(
					"delay consumer group %q topic %d is empty",
					group.Name,
					topicIndex,
				)
			}
		}
	}

	return nil
}

func levelQueueName(config DelayConfig, level int) string {
	return fmt.Sprintf("%s.%d", config.LevelQueuePrefix, level)
}

func levelRoutingKey(config DelayConfig, level int) string {
	return fmt.Sprintf("%s.%d", config.LevelRoutingPrefix, level)
}

// declareLevelExchanges 声明 Level 和 Dispatcher 使用的 direct Exchange。
func declareLevelExchanges(ch *amqp.Channel, config DelayConfig) error {
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
			return fmt.Errorf("declare delay exchange %q: %w", exchange, err)
		}
	}
	return nil
}

// declareDelayEventExchanges 声明正常广播、重试回投和死信 Exchange。
func declareDelayEventExchanges(ch *amqp.Channel, config DelayConfig) error {
	exchanges := []struct {
		name string
		kind string
	}{
		{name: config.TopicExchange, kind: "topic"},
		{name: config.RedriveExchange, kind: "direct"},
		{name: config.DeadLetterExchange, kind: "direct"},
	}

	for _, exchange := range exchanges {
		if err := ch.ExchangeDeclare(
			exchange.name,
			exchange.kind,
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("declare delay exchange %q: %w", exchange.name, err)
		}
	}
	return nil
}

// declareDispatcherInbox 声明 Dispatcher 持久化 Inbox。
func declareDispatcherInbox(ch *amqp.Channel, config DelayConfig) error {
	if _, err := ch.QueueDeclare(
		config.DispatcherQueue,
		true,
		false,
		false,
		false,
		amqp.Table{"x-queue-type": "quorum"},
	); err != nil {
		return fmt.Errorf("declare dispatcher queue %q: %w", config.DispatcherQueue, err)
	}
	if err := ch.QueueBind(
		config.DispatcherQueue,
		config.DispatcherRoutingKey,
		config.DispatcherExchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("bind dispatcher queue %q: %w", config.DispatcherQueue, err)
	}
	return nil
}

// declareLevelQueues 声明固定 TTL Queue，并让到期任务流入 Dispatcher Inbox。
func declareLevelQueues(ch *amqp.Channel, config DelayConfig) error {
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
			return fmt.Errorf("declare level queue %q: %w", queue, err)
		}
		if err := ch.QueueBind(
			queue,
			routingKey,
			config.LevelExchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("bind delay level queue %q: %w", queue, err)
		}
	}
	return nil
}

// declareConsumerGroupTopologies 声明每个消费者组的业务 Queue、DLQ 和 Binding。
func declareConsumerGroupTopologies(ch *amqp.Channel, config DelayConfig) error {
	for _, group := range config.ConsumerGroups {
		if _, err := ch.QueueDeclare(
			group.Queue,
			true,
			false,
			false,
			false,
			amqp.Table{
				"x-queue-type":              "quorum",
				"x-dead-letter-exchange":    config.DeadLetterExchange,
				"x-dead-letter-routing-key": group.DeadLetterRoutingKey,
			},
		); err != nil {
			return fmt.Errorf("declare consumer group queue %q: %w", group.Queue, err)
		}

		for _, topic := range group.Topics {
			if err := ch.QueueBind(group.Queue, topic, config.TopicExchange, false, nil); err != nil {
				return fmt.Errorf(
					"bind consumer group queue %q to topic %q: %w",
					group.Queue,
					topic,
					err,
				)
			}
		}

		if err := ch.QueueBind(
			group.Queue,
			group.Name,
			config.RedriveExchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("bind consumer group queue %q for redrive: %w", group.Queue, err)
		}

		if _, err := ch.QueueDeclare(
			group.DeadLetterQueue,
			true,
			false,
			false,
			false,
			amqp.Table{"x-queue-type": "quorum"},
		); err != nil {
			return fmt.Errorf("declare consumer group DLQ %q: %w", group.DeadLetterQueue, err)
		}

		if err := ch.QueueBind(
			group.DeadLetterQueue,
			group.DeadLetterRoutingKey,
			config.DeadLetterExchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("bind consumer group DLQ %q: %w", group.DeadLetterQueue, err)
		}
	}
	return nil
}

// declareDelayTopology 幂等声明统一延迟链路使用的全部 Exchange、Queue 和 Binding。
func declareDelayTopology(ch *amqp.Channel, config DelayConfig) error {
	if ch == nil {
		return errors.New("declare delay topology: channel is nil")
	}
	if err := validateDelayConfig(config); err != nil {
		return err
	}
	if err := declareLevelExchanges(ch, config); err != nil {
		return err
	}
	if err := declareDelayEventExchanges(ch, config); err != nil {
		return err
	}
	if err := declareDispatcherInbox(ch, config); err != nil {
		return err
	}
	if err := declareLevelQueues(ch, config); err != nil {
		return err
	}
	return declareConsumerGroupTopologies(ch, config)
}
