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
