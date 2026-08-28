package test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"GopherAI/config"
)

// TestAutoModelConfig_DecodesModelConnection 校验 auto 模型的连接配置来自自洽的 [autoModelConfig] 段。
func TestAutoModelConfig_DecodesModelConnection(t *testing.T) {
	var cfg config.Config
	_, err := toml.DecodeReader(strings.NewReader(`
[autoModelConfig]
modelName = "qwen-plus"
baseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
apiKey = "auto-api-key"
`), &cfg)
	if err != nil {
		t.Fatalf("DecodeReader() error = %v", err)
	}

	if cfg.AutoModelName != "qwen-plus" {
		t.Fatalf("AutoModelName = %q, want qwen-plus", cfg.AutoModelName)
	}
	if cfg.AutoBaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("AutoBaseURL = %q, want configured base URL", cfg.AutoBaseURL)
	}
	if cfg.AutoAPIKey != "auto-api-key" {
		t.Fatalf("AutoAPIKey = %q, want auto-api-key", cfg.AutoAPIKey)
	}
}

// TestRagModelConfig_DecodesIndependentAPIKey 校验 RAG 段拥有独立于 auto 的鉴权凭证。
func TestRagModelConfig_DecodesIndependentAPIKey(t *testing.T) {
	var cfg config.Config
	_, err := toml.DecodeReader(strings.NewReader(`
[ragModelConfig]
embeddingModel = "text-embedding-v4"
baseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
apiKey = "rag-api-key"
`), &cfg)
	if err != nil {
		t.Fatalf("DecodeReader() error = %v", err)
	}

	if cfg.RagEmbeddingModel != "text-embedding-v4" {
		t.Fatalf("RagEmbeddingModel = %q, want text-embedding-v4", cfg.RagEmbeddingModel)
	}
	if cfg.RagAPIKey != "rag-api-key" {
		t.Fatalf("RagAPIKey = %q, want rag-api-key", cfg.RagAPIKey)
	}
}

// TestDelayConfig_DecodesPipelineAndConsumerGroups 校验统一延迟链路及消费者组配置可以完整解码。
func TestDelayConfig_DecodesPipelineAndConsumerGroups(t *testing.T) {
	var cfg config.Config
	_, err := toml.DecodeReader(strings.NewReader(`
[delayConfig]
enabled = true
shortThresholdMs = 60000
maxDelayHours = 168
levelExchange = "gopherai.delay.level"
levelQueuePrefix = "gopherai.delay.level"
levelRoutingPrefix = "delay.level"
dispatcherExchange = "gopherai.delay.dispatcher"
dispatcherQueue = "gopherai.delay.dispatcher.inbox.v1"
dispatcherRoutingKey = "delay.dispatcher"
maxLevel = 60
confirmTimeoutMs = 3000
topicExchange = "gopherai.events"
redriveExchange = "gopherai.events.redrive"
deadLetterExchange = "gopherai.events.dlx"
dispatcherPrefetchCount = 128
dispatcherReadyCapacity = 128
pollIntervalMs = 200
pollAheadMs = 10000
leaseDurationMs = 30000
pollBatchSize = 200
publishWorkers = 16
pollerMaxLevel = 10

[[delayConfig.consumerGroups]]
name = "persistence"
queue = "gopherai.events.persistence.v1"
topics = ["chat.message.created.v1"]
deadLetterQueue = "gopherai.events.persistence.dlq.v1"
deadLetterRoutingKey = "persistence"
retryDelaysMs = [10000, 30000]
`), &cfg)
	if err != nil {
		t.Fatalf("DecodeReader() error = %v", err)
	}

	delayCfg := cfg.DelayConfig
	if !delayCfg.Enabled {
		t.Fatal("DelayConfig.Enabled = false, want true")
	}
	if delayCfg.ShortThresholdMs != 60000 || delayCfg.MaxDelayHours != 168 {
		t.Fatalf(
			"delay range = %dms/%dh, want 60000ms/168h",
			delayCfg.ShortThresholdMs,
			delayCfg.MaxDelayHours,
		)
	}
	if delayCfg.MaxLevel != 60 || delayCfg.PollerMaxLevel != 10 {
		t.Fatalf(
			"delay levels = %d/%d, want 60/10",
			delayCfg.MaxLevel,
			delayCfg.PollerMaxLevel,
		)
	}
	if delayCfg.DispatcherQueue != "gopherai.delay.dispatcher.inbox.v1" {
		t.Fatalf("DispatcherQueue = %q", delayCfg.DispatcherQueue)
	}
	if delayCfg.TopicExchange != "gopherai.events" ||
		delayCfg.RedriveExchange != "gopherai.events.redrive" {
		t.Fatalf(
			"final exchanges = %q/%q",
			delayCfg.TopicExchange,
			delayCfg.RedriveExchange,
		)
	}
	if delayCfg.DispatcherPrefetchCount != 128 || delayCfg.DispatcherReadyCapacity != 128 {
		t.Fatalf(
			"dispatcher capacities = %d/%d, want 128/128",
			delayCfg.DispatcherPrefetchCount,
			delayCfg.DispatcherReadyCapacity,
		)
	}
	if len(delayCfg.ConsumerGroups) != 1 {
		t.Fatalf("len(ConsumerGroups) = %d, want 1", len(delayCfg.ConsumerGroups))
	}

	group := delayCfg.ConsumerGroups[0]
	if group.Name != "persistence" || group.Queue != "gopherai.events.persistence.v1" {
		t.Fatalf("consumer group = %q/%q", group.Name, group.Queue)
	}
	if len(group.Topics) != 1 || group.Topics[0] != "chat.message.created.v1" {
		t.Fatalf("consumer group topics = %#v", group.Topics)
	}
	if len(group.RetryDelaysMs) != 2 || group.RetryDelaysMs[1] != 30000 {
		t.Fatalf("consumer group retry delays = %#v", group.RetryDelaysMs)
	}
}

// TestProjectDelayConfig 校验项目实际 TOML 已提供第一阶段所需的延迟配置。
func TestProjectDelayConfig(t *testing.T) {
	var cfg config.Config
	path := filepath.Join("..", "config", "config.toml")
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatalf("DecodeFile(%q) error = %v", path, err)
	}

	delayCfg := cfg.DelayConfig
	if !delayCfg.Enabled {
		t.Fatal("project delay config is disabled")
	}
	if delayCfg.DispatcherQueue == "" || delayCfg.TopicExchange == "" || delayCfg.RedriveExchange == "" {
		t.Fatal("project delay topology config is incomplete")
	}
	if delayCfg.DispatcherPrefetchCount <= 0 || delayCfg.DispatcherReadyCapacity <= 0 {
		t.Fatal("project dispatcher capacity config must be positive")
	}
	if len(delayCfg.ConsumerGroups) == 0 {
		t.Fatal("project delay consumer groups are empty")
	}
	if len(delayCfg.ConsumerGroups[0].RetryDelaysMs) == 0 {
		t.Fatal("project consumer group retry delays are empty")
	}
}

// TestBootstrapConstants_MigratedToConfig 校验原组合根硬编码常量已迁到配置段并正确解码。
func TestBootstrapConstants_MigratedToConfig(t *testing.T) {
	var cfg config.Config
	_, err := toml.DecodeReader(strings.NewReader(`
[redisConfig]
pingTimeoutMs = 5000

[rabbitmqConfig]
prefetchCount = 20

[mcpConfig]
baseUrl = "http://10.0.0.2:8081/mcp"

[imageServiceConfig]
modelPath = "/data/models/mobilenetv2.onnx"
labelPath = "/data/labels.txt"
`), &cfg)
	if err != nil {
		t.Fatalf("DecodeReader() error = %v", err)
	}

	if cfg.RedisPingTimeoutMs != 5000 {
		t.Fatalf("RedisPingTimeoutMs = %d, want 5000", cfg.RedisPingTimeoutMs)
	}
	if cfg.RabbitmqPrefetchCount != 20 {
		t.Fatalf("RabbitmqPrefetchCount = %d, want 20", cfg.RabbitmqPrefetchCount)
	}
	if cfg.McpConfig.BaseURL != "http://10.0.0.2:8081/mcp" {
		t.Fatalf("McpConfig.BaseURL = %q, want configured mcp base url", cfg.McpConfig.BaseURL)
	}
	if cfg.ImageModelPath != "/data/models/mobilenetv2.onnx" {
		t.Fatalf("ImageModelPath = %q, want configured model path", cfg.ImageModelPath)
	}
	if cfg.ImageLabelPath != "/data/labels.txt" {
		t.Fatalf("ImageLabelPath = %q, want configured label path", cfg.ImageLabelPath)
	}
}
