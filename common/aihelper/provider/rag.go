package provider

import (
	"GopherAI/common/logger"
	"GopherAI/common/rag"
	"GopherAI/config"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// AliRAGModel 表示带检索增强能力的模型实现。
type AliRAGModel struct {
	// llm 持有底层聊天模型实例。
	llm einomodel.ToolCallingChatModel
	// username 用于定位当前用户的知识文档。
	username string
}

// NewAliRAGModel 创建 RAG 模型实例。
func NewAliRAGModel(ctx context.Context, username string) (*AliRAGModel, error) {
	key := os.Getenv("OPENAI_API_KEY")
	conf := config.GetConfig()
	modelName := conf.RagModelConfig.RagChatModelName
	baseURL := conf.RagModelConfig.RagBaseUrl

	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		logger.Error("NewAliRAGModel failed", "user", username, "err", err)
		return nil, fmt.Errorf("create ali rag model failed: %v", err)
	}
	logger.Info("NewAliRAGModel success", "user", username, "model", modelName)
	return &AliRAGModel{llm: llm, username: username}, nil
}

// GenerateResponse 生成同步响应，并在可用时引入检索增强上下文。
func (o *AliRAGModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	// ragMessages 表示检索增强后的消息集合。
	ragMessages, err := o.buildRAGMessages(ctx, messages)
	if err != nil {
		logger.Error("AliRAGModel GenerateResponse buildRAGMessages failed", "user", o.username, "err", err)
		return nil, err
	}
	resp, err := o.llm.Generate(ctx, ragMessages)
	if err != nil {
		logger.Error("AliRAGModel GenerateResponse llm failed", "user", o.username, "err", err)
		return nil, fmt.Errorf("ali rag generate failed: %v", err)
	}
	return resp, nil
}

// StreamResponse 生成流式响应，并在可用时引入检索增强上下文。
func (o *AliRAGModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	ragMessages, err := o.buildRAGMessages(ctx, messages)
	if err != nil {
		logger.Warn("AliRAGModel StreamResponse fallback to raw messages", "user", o.username, "err", err)
		ragMessages = messages
	}
	stream, err := o.llm.Stream(ctx, ragMessages)
	if err != nil {
		logger.Error("AliRAGModel StreamResponse start failed", "user", o.username, "err", err)
		return "", fmt.Errorf("ali rag stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("AliRAGModel StreamResponse recv failed", "user", o.username, "err", err)
			return "", fmt.Errorf("ali rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return fullResp.String(), nil
}

// buildRAGMessages 构造检索增强后的消息集合。
func (o *AliRAGModel) buildRAGMessages(ctx context.Context, messages []*schema.Message) ([]*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	ragQuery, err := rag.NewRAGQuery(ctx, o.username)
	if err != nil {
		logger.Warn("AliRAGModel buildRAGMessages create query failed", "user", o.username, "err", err)
		return messages, nil
	}

	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content
	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		logger.Warn("AliRAGModel buildRAGMessages retrieve failed", "user", o.username, "err", err)
		return messages, nil
	}

	ragPrompt := rag.BuildRAGPrompt(query, docs)
	ragMessages := make([]*schema.Message, len(messages))
	copy(ragMessages, messages)
	ragMessages[len(ragMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: ragPrompt,
	}
	return ragMessages, nil
}

// GetModelType 返回模型类型标识。
func (o *AliRAGModel) GetModelType() string { return "2" }
