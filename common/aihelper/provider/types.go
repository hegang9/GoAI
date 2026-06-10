package provider

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// StreamCallback 定义流式输出时的分片回调函数。
type StreamCallback func(msg string)

// AIModel 定义统一的 AI 模型抽象接口。
type AIModel interface {
	// GenerateResponse 生成同步响应。
	GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error)
	// StreamResponse 生成流式响应，并通过回调返回分片内容。
	StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error)
	// GetModelType 返回模型类型标识。
	GetModelType() string
}
