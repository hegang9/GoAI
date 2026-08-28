package rabbitmq

import (
	"strings"
	"testing"
	"time"
)

func validDelayTopologyConfig() DelayConfig {
	return DelayConfig{
		LevelExchange:        "gopherai.delay.level",
		LevelQueuePrefix:     "gopherai.delay.level",
		LevelRoutingPrefix:   "delay.level",
		DispatcherExchange:   "gopherai.delay.dispatcher",
		DispatcherQueue:      "gopherai.delay.dispatcher.inbox.v1",
		DispatcherRoutingKey: "delay.dispatcher",
		TopicExchange:        "gopherai.events",
		RedriveExchange:      "gopherai.events.redrive",
		DeadLetterExchange:   "gopherai.events.dlx",
		ConsumerGroups: []DelayConsumerGroupConfig{
			{
				Name:                 "persistence",
				Queue:                "gopherai.events.persistence.v1",
				Topics:               []string{"chat.message.created.v1"},
				DeadLetterQueue:      "gopherai.events.persistence.dlq.v1",
				DeadLetterRoutingKey: "persistence",
			},
		},
		MaxLevel:       60,
		ConfirmTimeout: 3 * time.Second,
	}
}

func TestValidateDelayConfigAcceptsCompleteTopology(t *testing.T) {
	if err := validateDelayConfig(validDelayTopologyConfig()); err != nil {
		t.Fatalf("validateDelayConfig() error = %v", err)
	}
}

func TestDeclareDelayTopologyRejectsNilChannel(t *testing.T) {
	err := declareDelayTopology(nil, validDelayTopologyConfig())
	if err == nil || !strings.Contains(err.Error(), "channel is nil") {
		t.Fatalf("declareDelayTopology() error = %v, want nil channel error", err)
	}
}

func TestValidateDelayConfigRejectsInvalidEventTopology(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*DelayConfig)
		wantError string
	}{
		{
			name: "empty topic exchange",
			change: func(config *DelayConfig) {
				config.TopicExchange = ""
			},
			wantError: "topic exchange is empty",
		},
		{
			name: "empty redrive exchange",
			change: func(config *DelayConfig) {
				config.RedriveExchange = ""
			},
			wantError: "redrive exchange is empty",
		},
		{
			name: "empty dead letter exchange",
			change: func(config *DelayConfig) {
				config.DeadLetterExchange = ""
			},
			wantError: "dead letter exchange is empty",
		},
		{
			name: "empty consumer groups",
			change: func(config *DelayConfig) {
				config.ConsumerGroups = nil
			},
			wantError: "consumer groups are empty",
		},
		{
			name: "empty consumer group name",
			change: func(config *DelayConfig) {
				config.ConsumerGroups[0].Name = " "
			},
			wantError: "name is empty",
		},
		{
			name: "empty consumer group queue",
			change: func(config *DelayConfig) {
				config.ConsumerGroups[0].Queue = ""
			},
			wantError: "queue is empty",
		},
		{
			name: "empty consumer group topics",
			change: func(config *DelayConfig) {
				config.ConsumerGroups[0].Topics = nil
			},
			wantError: "topics are empty",
		},
		{
			name: "blank consumer group topic",
			change: func(config *DelayConfig) {
				config.ConsumerGroups[0].Topics[0] = " "
			},
			wantError: "topic 0 is empty",
		},
		{
			name: "empty dead letter queue",
			change: func(config *DelayConfig) {
				config.ConsumerGroups[0].DeadLetterQueue = ""
			},
			wantError: "dead letter queue is empty",
		},
		{
			name: "empty dead letter routing key",
			change: func(config *DelayConfig) {
				config.ConsumerGroups[0].DeadLetterRoutingKey = ""
			},
			wantError: "dead letter routing key is empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validDelayTopologyConfig()
			test.change(&config)

			err := validateDelayConfig(config)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateDelayConfig() error = %v, want %q", err, test.wantError)
			}
		})
	}
}
