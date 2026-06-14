package rag

import (
	"GopherAI/config"
	"context"
	"fmt"
	"os"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	"github.com/cloudwego/eino/components/embedding"
)

// newEmbedder 创建向量生成器（embedding）。
//
// 作用：把“人能读的文本”翻译成“AI 能按语义检索的向量表示”。
// 索引端（写入）与检索端（查询）共用同一构建逻辑，仅模型名称可能不同，
// 因此抽取为内部公共函数，避免重复代码。
func newEmbedder(ctx context.Context, model string) (embedding.Embedder, error) {
	cfg := config.GetConfig()
	// 调用向量模型所需的 API Key 从环境变量读取。
	apiKey := os.Getenv("OPENAI_API_KEY")

	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: cfg.RagModelConfig.RagBaseUrl, // 向量模型服务地址
		APIKey:  apiKey,                        // 鉴权信息
		Model:   model,                         // 使用哪个向量模型
	}

	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}
	return embedder, nil
}
