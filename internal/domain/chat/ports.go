package chat

import "context"

// StreamCallback 定义流式输出时的分片回调函数。
type StreamCallback func(chunk string)

// Model 是统一的 AI 模型领域端口。
//
// 它只使用领域类型（[]Message、string），不暴露任何底层 SDK（如 eino schema）的细节；
// 具体的 OpenAI / Ollama / RAG / MCP 适配器在 infrastructure/ai 中实现该接口。
type Model interface {
	// Generate 基于会话历史生成完整回复内容。
	Generate(ctx context.Context, history []Message) (string, error)
	// Stream 基于会话历史流式生成回复，分片通过 cb 回调输出，返回聚合后的完整内容。
	Stream(ctx context.Context, history []Message, cb StreamCallback) (string, error)
	// Type 返回模型类型标识（"1" OpenAI / "2" RAG / "3" MCP / "4" Ollama）。
	Type() string
}

// ModelFactory 是模型创建端口：根据模型类型与参数创建具体 Model 实现。
// 由 infrastructure/ai 实现，领域层据此创建会话所需的模型而无需感知具体实现。
type ModelFactory interface {
	// Create 根据模型类型与参数创建模型实例。
	Create(ctx context.Context, modelType string, params map[string]any) (Model, error)
}

// MessageSink 是会话聚合用于“异步持久化消息”的端口。
//
// 会话聚合只负责把消息交给 Sink，不关心其背后是 RabbitMQ 还是直接落库，
// 由此消除了领域对消息队列的直接依赖。
type MessageSink interface {
	// Save 异步保存一条消息（典型实现为发布到消息队列）。
	Save(msg Message) error
}

// MessageRepository 是消息持久化端口（最终写库），由 infrastructure/persistence 实现。
type MessageRepository interface {
	// Create 持久化一条消息。
	Create(ctx context.Context, msg Message) error
	// ListAll 读取全部历史消息，按时间、ID 升序返回，用于启动时回放会话上下文。
	ListAll(ctx context.Context) ([]Message, error)
}

// SessionRepository 是会话持久化端口，由 infrastructure/persistence 实现。
type SessionRepository interface {
	// Create 持久化创建一条会话。
	Create(ctx context.Context, s Session) (Session, error)
	// ListByAccount 查询指定账号的全部会话。
	ListByAccount(ctx context.Context, accountNo string) ([]Session, error)
}
