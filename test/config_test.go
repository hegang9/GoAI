package test

import (
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

// TestBootstrapConstants_MigratedToConfig 校验原组合根硬编码常量已迁到配置段并正确解码。
func TestBootstrapConstants_MigratedToConfig(t *testing.T) {
	var cfg config.Config
	_, err := toml.DecodeReader(strings.NewReader(`
[redisConfig]
pingTimeoutMs = 5000

[rabbitmqConfig]
mainExchange = "gopherai.chat"
mainQueue = "gopherai.chat.persist.v1"
mainRoutingKey = "chat.message.persist.v1"
retryExchange = "gopherai.chat.retry"
localRetryDelaysMs = [100, 500]
retryJitterPercent = 25
maxRetries = 5
deadLetterExchange = "gopherai.chat.dlx"
deadLetterQueue = "gopherai.chat.persist.dlq.v1"
deadLetterRoutingKey = "chat.message.persist.dead.v1"
prefetchCount = 20
publishConfirmTimeoutMs = 3000

[[rabbitmqConfig.retryTiers]]
queue = "gopherai.chat.persist.retry.1.v1"
routingKey = "chat.message.persist.retry.1"
delayMs = 10000

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
	if cfg.RabbitmqMainQueue != "gopherai.chat.persist.v1" {
		t.Fatalf("RabbitmqMainQueue = %q, want gopherai.chat.persist.v1", cfg.RabbitmqMainQueue)
	}
	if len(cfg.RabbitmqRetryTiers) != 1 {
		t.Fatalf("len(RabbitmqRetryTiers) = %d, want 1", len(cfg.RabbitmqRetryTiers))
	}
	if cfg.RabbitmqRetryTiers[0].DelayMs != 10000 {
		t.Fatalf("retry tier delay = %d, want 10000", cfg.RabbitmqRetryTiers[0].DelayMs)
	}
	if cfg.RabbitmqDeadLetterQueue != "gopherai.chat.persist.dlq.v1" {
		t.Fatalf("RabbitmqDeadLetterQueue = %q, want gopherai.chat.persist.dlq.v1", cfg.RabbitmqDeadLetterQueue)
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
