package ai

import (
	"context"
	"fmt"
	"io"
	"strings"

	"GopherAI/internal/domain/chat"
	"GopherAI/pkg/logger"

	ollamaext "github.com/cloudwego/eino-ext/components/model/ollama"
	einomodel "github.com/cloudwego/eino/components/model"
)

// OllamaModel 基于 Ollama 实现 domain/chat.Model 端口。
type OllamaModel struct {
	llm einomodel.ToolCallingChatModel
}

// NewOllamaModel 创建 Ollama 模型实例。
func NewOllamaModel(ctx context.Context, baseURL, modelName string) (*OllamaModel, error) {
	llm, err := ollamaext.NewChatModel(ctx, &ollamaext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
	})
	if err != nil {
		logger.Error("NewOllamaModel failed", "model", modelName, "baseURL", baseURL, "err", err)
		return nil, fmt.Errorf("create ollama model failed: %v", err)
	}
	logger.Info("NewOllamaModel success", "model", modelName, "baseURL", baseURL)
	return &OllamaModel{llm: llm}, nil
}

// 编译期断言：OllamaModel 必须满足领域模型端口。
var _ chat.Model = (*OllamaModel)(nil)

// Generate 生成同步回复内容。
func (o *OllamaModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	resp, err := o.llm.Generate(ctx, toSchemaMessages(history))
	if err != nil {
		logger.Error("OllamaModel Generate failed", "err", err)
		return "", fmt.Errorf("ollama generate failed: %v", err)
	}
	return resp.Content, nil
}

// Stream 生成流式回复并聚合完整内容。
func (o *OllamaModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, toSchemaMessages(history))
	if err != nil {
		logger.Error("OllamaModel Stream start failed", "err", err)
		return "", fmt.Errorf("ollama stream failed: %v", err)
	}
	defer stream.Close()

	var full strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("OllamaModel Stream recv failed", "err", err)
			return "", fmt.Errorf("ollama stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			full.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return full.String(), nil
}

// Type 返回模型类型标识。
func (o *OllamaModel) Type() string { return "4" }
