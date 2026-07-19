package test

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"GopherAI/config"
)

// TestAIModelConfig_DecodesModelConnection 校验普通 OpenAI 兼容模型的连接配置来自统一配置段。
func TestAIModelConfig_DecodesModelConnection(t *testing.T) {
	var cfg config.Config
	_, err := toml.DecodeReader(strings.NewReader(`
[aiModelConfig]
modelName = "qwen-plus"
baseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
apiKey = "test-api-key"
`), &cfg)
	if err != nil {
		t.Fatalf("DecodeReader() error = %v", err)
	}

	if cfg.AIModelName != "qwen-plus" {
		t.Fatalf("AIModelName = %q, want qwen-plus", cfg.AIModelName)
	}
	if cfg.AIBaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("AIBaseURL = %q, want configured base URL", cfg.AIBaseURL)
	}
	if cfg.APIKey != "test-api-key" {
		t.Fatalf("APIKey = %q, want test-api-key", cfg.APIKey)
	}
}

// TestBootstrapConstants_MigratedToConfig 校验原组合根硬编码常量已迁到配置段并正确解码。
func TestBootstrapConstants_MigratedToConfig(t *testing.T) {
	var cfg config.Config
	_, err := toml.DecodeReader(strings.NewReader(`
[redisConfig]
pingTimeoutMs = 5000

[rabbitmqConfig]
queue = "ChatMessage"

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
	if cfg.RabbitmqQueue != "ChatMessage" {
		t.Fatalf("RabbitmqQueue = %q, want ChatMessage", cfg.RabbitmqQueue)
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
