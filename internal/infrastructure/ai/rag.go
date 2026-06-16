package ai

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"GopherAI/internal/domain/chat"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/internal/infrastructure/storage"
	"GopherAI/pkg/logger"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// RAGModel 在普通对话外层叠加检索增强，实现 domain/chat.Model 端口。
type RAGModel struct {
	llm       einomodel.ToolCallingChatModel
	accountNo string
	engine    *raginfra.Engine
}

// NewRAGModel 创建 RAG 模型实例。
func NewRAGModel(ctx context.Context, accountNo, modelName, baseURL string, engine *raginfra.Engine) (*RAGModel, error) {
	key := os.Getenv("OPENAI_API_KEY")
	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		logger.Error("NewRAGModel failed", "accountNo", accountNo, "err", err)
		return nil, fmt.Errorf("create rag model failed: %v", err)
	}
	logger.Info("NewRAGModel success", "accountNo", accountNo, "model", modelName)
	return &RAGModel{llm: llm, accountNo: accountNo, engine: engine}, nil
}

// 编译期断言：RAGModel 必须满足领域模型端口。
var _ chat.Model = (*RAGModel)(nil)

// Generate 生成同步回复，并在可用时引入检索增强上下文。
func (o *RAGModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	messages := o.buildRAGMessages(ctx, toSchemaMessages(history))
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		logger.Error("RAGModel Generate failed", "accountNo", o.accountNo, "err", err)
		return "", fmt.Errorf("rag generate failed: %v", err)
	}
	return resp.Content, nil
}

// Stream 生成流式回复，并在可用时引入检索增强上下文。
func (o *RAGModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	messages := o.buildRAGMessages(ctx, toSchemaMessages(history))
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		logger.Error("RAGModel Stream start failed", "accountNo", o.accountNo, "err", err)
		return "", fmt.Errorf("rag stream failed: %v", err)
	}
	defer stream.Close()

	var full strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("RAGModel Stream recv failed", "accountNo", o.accountNo, "err", err)
			return "", fmt.Errorf("rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			full.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return full.String(), nil
}

// buildRAGMessages 解析当前账号文档、检索相关上下文并替换最后一条用户消息。
// 任意环节失败时回退到原始消息，保证对话仍可继续。
func (o *RAGModel) buildRAGMessages(ctx context.Context, messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}

	filename, err := storage.ResolveUserDocFilename(o.accountNo)
	if err != nil {
		logger.Warn("RAGModel resolve file failed", "accountNo", o.accountNo, "err", err)
		return messages
	}

	query := messages[len(messages)-1].Content
	prompt, err := o.engine.Retrieve(ctx, filename, query)
	if err != nil {
		logger.Warn("RAGModel retrieve failed", "accountNo", o.accountNo, "err", err)
		return messages
	}

	ragMessages := make([]*schema.Message, len(messages))
	copy(ragMessages, messages)
	ragMessages[len(ragMessages)-1] = &schema.Message{Role: schema.User, Content: prompt}
	return ragMessages
}

// Type 返回模型类型标识。
func (o *RAGModel) Type() string { return "2" }
