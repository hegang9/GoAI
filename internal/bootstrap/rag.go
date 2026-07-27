package bootstrap

import (
	"context"
	"fmt"

	"GopherAI/config"
	redisstore "GopherAI/internal/infrastructure/cache/redis"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/pkg/logger"
)

// newRAGEngine 集中生产服务与离线评测共用的 RAG 依赖装配。
func newRAGEngine(ctx context.Context, conf *config.Config, vectorStore *redisstore.VectorStore) (*raginfra.Engine, error) {
	var reranker raginfra.Reranker
	if conf.RagRerankEnable {
		reranker = raginfra.NewHTTPReranker(conf.RagRerankBaseUrl, conf.RagAPIKey, conf.RagRerankModel)
		logger.Info("rag reranker init success", "model", conf.RagRerankModel, "baseURL", conf.RagRerankBaseUrl)
	}
	engine, err := raginfra.NewEngine(ctx, raginfra.Config{
		EmbeddingModel: conf.RagEmbeddingModel, BaseURL: conf.RagBaseUrl, APIKey: conf.RagAPIKey,
		Dimension: conf.RagDimension, ChunkSize: conf.RagChunkSize, ChunkOverlap: conf.RagChunkOverlap,
		TopK: conf.RagTopK, MaxDistance: conf.RagMaxDistance, RecallTopK: conf.RagRecallTopK,
		RerankTopK: conf.RagRerankTopK, RerankEnable: conf.RagRerankEnable,
		RerankMinScore: conf.RagRerankMinScore, EnableSemanticChunking: conf.RagEnableSemanticChunking,
		SemanticPercentile: conf.RagSemanticBreakpointPercentile, SemanticBufferSize: conf.RagSemanticBufferSize,
		ContextWindow: conf.RagContextWindow, EnableHeaderInjection: conf.RagEnableHeaderInjection,
	}, vectorStore, reranker)
	if err != nil {
		return nil, fmt.Errorf("init rag engine failed: %w", err)
	}
	return engine, nil
}
