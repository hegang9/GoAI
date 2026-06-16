package ai

import (
	"context"
	"fmt"

	"GopherAI/internal/domain/chat"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/pkg/logger"
)

// FactoryConfig 描述模型工厂创建各类模型所需的配置。
type FactoryConfig struct {
	// ChatModelName RAG / MCP 模型使用的对话模型名称。
	ChatModelName string
	// BaseURL RAG / MCP 模型对话 API 基础地址。
	BaseURL string
	// MCPBaseURL MCP 服务地址。
	MCPBaseURL string
}

// Factory 实现 domain/chat.ModelFactory 端口：按模型类型创建具体模型实现。
type Factory struct {
	cfg    FactoryConfig
	engine *raginfra.Engine
}

// NewFactory 创建模型工厂。
func NewFactory(cfg FactoryConfig, engine *raginfra.Engine) *Factory {
	return &Factory{cfg: cfg, engine: engine}
}

// 编译期断言：Factory 必须满足领域模型工厂端口。
var _ chat.ModelFactory = (*Factory)(nil)

// Create 根据模型类型与参数创建模型实例。
//
// 类型约定："1" OpenAI、"2" RAG、"3" MCP、"4" Ollama；
// "2"/"3" 需要 params["account_no"]，"4" 需要 params["modelName"]/"baseURL"。
func (f *Factory) Create(ctx context.Context, modelType string, params map[string]any) (chat.Model, error) {
	switch modelType {
	case "1":
		return NewOpenAIModel(ctx)
	case "2":
		accountNo, ok := params["account_no"].(string)
		if !ok {
			return nil, fmt.Errorf("RAG model requires account_no")
		}
		return NewRAGModel(ctx, accountNo, f.cfg.ChatModelName, f.cfg.BaseURL, f.engine)
	case "3":
		accountNo, ok := params["account_no"].(string)
		if !ok {
			return nil, fmt.Errorf("MCP model requires account_no")
		}
		return NewMCPModel(ctx, accountNo, f.cfg.ChatModelName, f.cfg.BaseURL, f.cfg.MCPBaseURL)
	case "4":
		baseURL, _ := params["baseURL"].(string)
		modelName, ok := params["modelName"].(string)
		if !ok {
			return nil, fmt.Errorf("Ollama model requires modelName")
		}
		return NewOllamaModel(ctx, baseURL, modelName)
	default:
		logger.Error("Factory unsupported model type", "modelType", modelType)
		return nil, fmt.Errorf("unsupported model type: %s", modelType)
	}
}
