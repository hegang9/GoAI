package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// 描述 RabbitMQ 的拓扑，确保与控制台配置一致
// 如果这里返回 PRECONDITION_FAILED，说明控制台中的 Queue 参数和代码不一致

func declareTopology(ch *amqp.Channel, cfg Config) error {
	exchanges := []string{
		cfg.MainExchange,
		cfg.RetryExchange,
		cfg.DeadLetterExchange,
	}

	for _, exchange := range exchanges {
		if err := ch.ExchangeDeclare(
			exchange,
			"direct",
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("declare exchange %q: %w", exchange, err)
		}
	}

	if _, err := ch.QueueDeclare(
		cfg.MainQueue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    cfg.DeadLetterExchange,
			"x-dead-letter-routing-key": cfg.DeadLetterRoutingKey,
		},
	); err != nil {
		return fmt.Errorf("declare main queue: %w", err)
	}

	if err := ch.QueueBind(
		cfg.MainQueue,
		cfg.MainRoutingKey,
		cfg.MainExchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("bind main queue: %w", err)
	}

	if _, err := ch.QueueDeclare(
		cfg.DeadLetterQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare dead-letter queue: %w", err)
	}

	if err := ch.QueueBind(
		cfg.DeadLetterQueue,
		cfg.DeadLetterRoutingKey,
		cfg.DeadLetterExchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("bind dead-letter queue: %w", err)
	}

	for _, tier := range cfg.RetryTiers {
		maxTTL := tier.DelayMs * (100 + cfg.RetryJitterPercent) / 100

		if _, err := ch.QueueDeclare(
			tier.Queue,
			true,
			false,
			false,
			false,
			amqp.Table{
				"x-message-ttl":             int32(maxTTL),
				"x-dead-letter-exchange":    cfg.MainExchange,
				"x-dead-letter-routing-key": cfg.MainRoutingKey,
			},
		); err != nil {
			return fmt.Errorf("declare retry queue %q: %w", tier.Queue, err)
		}

		if err := ch.QueueBind(
			tier.Queue,
			tier.RoutingKey,
			cfg.RetryExchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("bind retry queue %q: %w", tier.Queue, err)
		}
	}

	return nil
}
