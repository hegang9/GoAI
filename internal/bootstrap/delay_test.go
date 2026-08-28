package bootstrap

import (
	"strings"
	"testing"
	"time"

	"GopherAI/config"
)

func TestMapDelayRuntimeConfig(t *testing.T) {
	source := validDelayConfig()
	source.ConsumerGroups = append(source.ConsumerGroups, config.DelayConsumerGroupConfig{
		Name:                 "analytics",
		Queue:                "gopherai.events.analytics.v1",
		Topics:               []string{"chat.message.created.v1", "chat.message.updated.v1"},
		DeadLetterQueue:      "gopherai.events.analytics.dlq.v1",
		DeadLetterRoutingKey: "analytics",
		RetryDelaysMs:        []int{1_000},
	})

	runtimeConfig, err := mapDelayRuntimeConfig(source, 20, "poller-1")
	if err != nil {
		t.Fatalf("mapDelayRuntimeConfig() error = %v", err)
	}
	if runtimeConfig.rabbit.ConfirmTimeout != 3*time.Second ||
		runtimeConfig.service.ShortThreshold != time.Minute ||
		runtimeConfig.service.MaxDelay != 7*24*time.Hour {
		t.Fatalf(
			"durations = confirm:%s threshold:%s max:%s",
			runtimeConfig.rabbit.ConfirmTimeout,
			runtimeConfig.service.ShortThreshold,
			runtimeConfig.service.MaxDelay,
		)
	}
	if runtimeConfig.poller.Owner != "poller-1" ||
		runtimeConfig.poller.Ahead != 10*time.Second ||
		runtimeConfig.poller.MaxLevel != 10 {
		t.Fatalf("poller config = %+v", runtimeConfig.poller)
	}
	if runtimeConfig.groupPrefetch != 20 ||
		len(runtimeConfig.rabbit.ConsumerGroups) != 2 ||
		len(runtimeConfig.final.ConsumerGroups) != 2 {
		t.Fatalf("consumer config = %+v", runtimeConfig)
	}
	if len(runtimeConfig.final.Topics) != 2 {
		t.Fatalf("deduplicated topics = %v", runtimeConfig.final.Topics)
	}
	if got := runtimeConfig.service.RetryPolicies["persistence"].Delays; len(got) != 2 || got[1] != 30*time.Second {
		t.Fatalf("persistence retry policy = %v", got)
	}
}

func TestMapDelayRuntimeConfigRejectsCrossComponentMismatch(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*config.DelayConfig)
		prefetch  int
		wantError string
	}{
		{
			name: "threshold does not match max level",
			change: func(source *config.DelayConfig) {
				source.ShortThresholdMs = 30_000
			},
			prefetch:  20,
			wantError: "short threshold must equal max level coverage",
		},
		{
			name: "poller level exceeds topology",
			change: func(source *config.DelayConfig) {
				source.PollerMaxLevel = 61
			},
			prefetch:  20,
			wantError: "poller max level exceeds delay topology",
		},
		{
			name:      "invalid group prefetch",
			change:    func(*config.DelayConfig) {},
			prefetch:  0,
			wantError: "group consumer prefetch must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := validDelayConfig()
			test.change(&source)
			_, err := mapDelayRuntimeConfig(source, test.prefetch, "poller-1")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("mapDelayRuntimeConfig() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestMapDelayRuntimeConfigRejectsMissingChatMessageTopic(t *testing.T) {
	source := validDelayConfig()
	source.ConsumerGroups[0].Topics = []string{"chat.message.updated.v1"}

	_, err := mapDelayRuntimeConfig(source, 20, "poller-1")
	if err == nil || !strings.Contains(err.Error(), "required topic") {
		t.Fatalf("mapDelayRuntimeConfig() error = %v, want required topic error", err)
	}
}

func validDelayConfig() config.DelayConfig {
	return config.DelayConfig{
		Enabled:                 true,
		ShortThresholdMs:        60_000,
		MaxDelayHours:           168,
		LevelExchange:           "gopherai.delay.level",
		LevelQueuePrefix:        "gopherai.delay.level",
		LevelRoutingPrefix:      "delay.level",
		DispatcherExchange:      "gopherai.delay.dispatcher",
		DispatcherQueue:         "gopherai.delay.dispatcher.inbox.v1",
		DispatcherRoutingKey:    "delay.dispatcher",
		MaxLevel:                60,
		ConfirmTimeoutMs:        3_000,
		TopicExchange:           "gopherai.events",
		RedriveExchange:         "gopherai.events.redrive",
		DeadLetterExchange:      "gopherai.events.dlx",
		DispatcherPrefetchCount: 128,
		DispatcherReadyCapacity: 128,
		PollIntervalMs:          200,
		PollAheadMs:             10_000,
		LeaseDurationMs:         30_000,
		PollBatchSize:           200,
		PublishWorkers:          16,
		PollerMaxLevel:          10,
		ConsumerGroups: []config.DelayConsumerGroupConfig{
			{
				Name:                 "persistence",
				Queue:                "gopherai.events.persistence.v1",
				Topics:               []string{"chat.message.created.v1"},
				DeadLetterQueue:      "gopherai.events.persistence.dlq.v1",
				DeadLetterRoutingKey: "persistence",
				RetryDelaysMs:        []int{10_000, 30_000},
			},
		},
	}
}
