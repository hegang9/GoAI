package provider

import (
	"GopherAI/common/logger"
	"context"
	"fmt"
	"io"
	"strings"

	ollamaext "github.com/cloudwego/eino-ext/components/model/ollama"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// OllamaModel 表示 Ollama 模型实现。
type OllamaModel struct {
	// llm 持有底层聊天模型实例。
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

// GenerateResponse 生成同步响应。
func (o *OllamaModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		logger.Error("OllamaModel GenerateResponse failed", "err", err)
		return nil, fmt.Errorf("ollama generate failed: %v", err)
	}
	return resp, nil
}

// StreamResponse 生成流式响应并聚合完整内容。
func (o *OllamaModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		logger.Error("OllamaModel StreamResponse start failed", "err", err)
		return "", fmt.Errorf("ollama stream failed: %v", err)
	}
	defer stream.Close()

	// fullResp 聚合完整响应，便于后续持久化。
	var fullResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("OllamaModel StreamResponse recv failed", "err", err)
			return "", fmt.Errorf("ollama stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return fullResp.String(), nil
}

// GetModelType 返回模型类型标识。
func (o *OllamaModel) GetModelType() string { return "4" }
