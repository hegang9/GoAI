package factory

import (
	providerpkg "GopherAI/common/aihelper/provider"
	sessionpkg "GopherAI/common/aihelper/session"
	"GopherAI/common/logger"
	"context"
	"fmt"
	"sync"
)

// ModelCreator 定义模型创建函数类型。
type ModelCreator func(ctx context.Context, config map[string]interface{}) (providerpkg.AIModel, error)

// AIModelFactory 表示 AI 模型工厂。
type AIModelFactory struct {
	// creators 保存模型类型到创建函数的映射。
	creators map[string]ModelCreator
}

var (
	globalFactory *AIModelFactory
	factoryOnce   sync.Once
)

// GetGlobalFactory 获取全局工厂实例。
func GetGlobalFactory() *AIModelFactory {
	factoryOnce.Do(func() {
		globalFactory = &AIModelFactory{creators: make(map[string]ModelCreator)}
		globalFactory.registerCreators()
		logger.Info("GetGlobalFactory init success")
	})
	return globalFactory
}

// registerCreators 注册所有内置模型创建器。
func (f *AIModelFactory) registerCreators() {
	f.creators["1"] = func(ctx context.Context, config map[string]interface{}) (providerpkg.AIModel, error) {
		return providerpkg.NewOpenAIModel(ctx)
	}
	f.creators["2"] = func(ctx context.Context, config map[string]interface{}) (providerpkg.AIModel, error) {
		accountNo, ok := config["account_no"].(string)
		if !ok {
			return nil, fmt.Errorf("RAG model requires account_no")
		}
		return providerpkg.NewAliRAGModel(ctx, accountNo)
	}
	f.creators["3"] = func(ctx context.Context, config map[string]interface{}) (providerpkg.AIModel, error) {
		accountNo, ok := config["account_no"].(string)
		if !ok {
			return nil, fmt.Errorf("MCP model requires account_no")
		}
		return providerpkg.NewMCPModel(ctx, accountNo)
	}
	f.creators["4"] = func(ctx context.Context, config map[string]interface{}) (providerpkg.AIModel, error) {
		baseURL, _ := config["baseURL"].(string)
		modelName, ok := config["modelName"].(string)
		if !ok {
			return nil, fmt.Errorf("Ollama model requires modelName")
		}
		return providerpkg.NewOllamaModel(ctx, baseURL, modelName)
	}
}

// CreateAIModel 根据类型创建 AI 模型。
func (f *AIModelFactory) CreateAIModel(ctx context.Context, modelType string, config map[string]interface{}) (providerpkg.AIModel, error) {
	// 获取模型对应的创建函数
	creator, ok := f.creators[modelType]
	if !ok {
		logger.Error("CreateAIModel unsupported model type", "modelType", modelType)
		return nil, fmt.Errorf("unsupported model type: %s", modelType)
	}
	model, err := creator(ctx, config)
	if err != nil {
		logger.Error("CreateAIModel creator failed", "modelType", modelType, "err", err)
		return nil, err
	}
	return model, nil
}

// CreateAIHelper 创建 AI 助手实例。
func (f *AIModelFactory) CreateAIHelper(ctx context.Context, modelType string, sessionID string, config map[string]interface{}) (*sessionpkg.AIHelper, error) {
	model, err := f.CreateAIModel(ctx, modelType, config)
	if err != nil {
		return nil, err
	}
	return sessionpkg.NewAIHelper(model, sessionID), nil
}

// RegisterModel 注册自定义模型。
func (f *AIModelFactory) RegisterModel(modelType string, creator ModelCreator) {
	f.creators[modelType] = creator
	logger.Info("RegisterModel success", "modelType", modelType)
}
