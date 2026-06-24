package ai

import (
	"context"
	"fmt"
	"io"
	"strings"

	"GopherAI/internal/domain/chat"
	"GopherAI/pkg/logger"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

// OpenAIModel 基于 OpenAI 兼容接口实现 domain/chat.Model 端口。
type OpenAIModel struct {
	llm einomodel.ToolCallingChatModel
}

// NewOpenAIModel 创建 OpenAI 兼容模型实例，连接配置由统一配置注入。
func NewOpenAIModel(ctx context.Context, modelName, baseURL, apiKey string) (*OpenAIModel, error) {
	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  apiKey,
	})
	if err != nil {
		logger.Error("NewOpenAIModel failed", "err", err)
		return nil, fmt.Errorf("create openai model failed: %v", err)
	}
	logger.Info("NewOpenAIModel success", "model", modelName, "baseURL", baseURL)
	return &OpenAIModel{llm: llm}, nil
}

// 编译期断言：OpenAIModel 必须满足领域模型端口。
var _ chat.Model = (*OpenAIModel)(nil)

// Generate 生成同步回复内容。
func (o *OpenAIModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	resp, err := o.llm.Generate(ctx, toSchemaMessages(history))
	if err != nil {
		logger.Error("OpenAIModel Generate failed", "err", err)
		return "", fmt.Errorf("openai generate failed: %v", err)
	}
	return resp.Content, nil
}

// Stream 生成流式回复并聚合完整内容。
func (o *OpenAIModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, toSchemaMessages(history))
	if err != nil {
		logger.Error("OpenAIModel Stream start failed", "err", err)
		return "", fmt.Errorf("openai stream failed: %v", err)
	}
	defer stream.Close()

	var full strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("OpenAIModel Stream recv failed", "err", err)
			return "", fmt.Errorf("openai stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			full.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return full.String(), nil
}

// Type 返回模型类型标识。
func (o *OpenAIModel) Type() string { return "1" }
