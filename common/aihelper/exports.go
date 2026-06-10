package aihelper

import (
	factorypkg "GopherAI/common/aihelper/factory"
	managerpkg "GopherAI/common/aihelper/manager"
	providerpkg "GopherAI/common/aihelper/provider"
	sessionpkg "GopherAI/common/aihelper/session"
)

// StreamCallback 兼容导出流式回调类型。
type StreamCallback = providerpkg.StreamCallback

// AIModel 兼容导出模型接口。
type AIModel = providerpkg.AIModel

// AIHelper 兼容导出会话助手类型。
type AIHelper = sessionpkg.AIHelper

// AIModelFactory 兼容导出模型工厂类型。
type AIModelFactory = factorypkg.AIModelFactory

// ModelCreator 兼容导出模型创建器类型。
type ModelCreator = factorypkg.ModelCreator

// AIHelperManager 兼容导出管理器类型。
type AIHelperManager = managerpkg.AIHelperManager

// AIToolCall 兼容导出 MCP 工具调用结构。
type AIToolCall = providerpkg.AIToolCall

// NewAIHelper 兼容导出会话助手构造函数。
var NewAIHelper = sessionpkg.NewAIHelper

// NewAIHelperManager 兼容导出管理器构造函数。
var NewAIHelperManager = managerpkg.NewAIHelperManager

// GetGlobalFactory 兼容导出全局工厂入口。
var GetGlobalFactory = factorypkg.GetGlobalFactory

// GetGlobalManager 兼容导出全局管理器入口。
var GetGlobalManager = managerpkg.GetGlobalManager

// NewOpenAIModel 兼容导出 OpenAI 模型构造函数。
var NewOpenAIModel = providerpkg.NewOpenAIModel

// NewOllamaModel 兼容导出 Ollama 模型构造函数。
var NewOllamaModel = providerpkg.NewOllamaModel

// NewAliRAGModel 兼容导出 RAG 模型构造函数。
var NewAliRAGModel = providerpkg.NewAliRAGModel

// NewMCPModel 兼容导出 MCP 模型构造函数。
var NewMCPModel = providerpkg.NewMCPModel
