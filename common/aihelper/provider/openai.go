package provider

import (
	"GopherAI/common/logger"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// OpenAIModel 表示 OpenAI 兼容模型实现。
type OpenAIModel struct {
	// llm 持有底层聊天模型实例。
	llm einomodel.ToolCallingChatModel
}

// NewOpenAIModel 创建 OpenAI 兼容模型实例。
func NewOpenAIModel(ctx context.Context) (*OpenAIModel, error) {
	key := os.Getenv("OPENAI_API_KEY")
	modelName := os.Getenv("OPENAI_MODEL_NAME")
	baseURL := os.Getenv("OPENAI_BASE_URL")

	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		logger.Error("NewOpenAIModel failed", "err", err)
		return nil, fmt.Errorf("create openai model failed: %v", err)
	}
	logger.Info("NewOpenAIModel success", "model", modelName, "baseURL", baseURL)
	return &OpenAIModel{llm: llm}, nil
}

// GenerateResponse 生成同步响应。
func (o *OpenAIModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		logger.Error("OpenAIModel GenerateResponse failed", "err", err)
		return nil, fmt.Errorf("openai generate failed: %v", err)
	}
	return resp, nil
}

// StreamResponse 生成流式响应并聚合完整内容。
func (o *OpenAIModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		logger.Error("OpenAIModel StreamResponse start failed", "err", err)
		return "", fmt.Errorf("openai stream failed: %v", err)
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
			logger.Error("OpenAIModel StreamResponse recv failed", "err", err)
			return "", fmt.Errorf("openai stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return fullResp.String(), nil
}

// GetModelType 返回模型类型标识。
func (o *OpenAIModel) GetModelType() string { return "1" }
