package bootstrap

import (
	"GopherAI/config"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/pkg/logger"
)

// ragEngineConfig 集中生产服务与离线评测共用的配置映射，不封装 Engine 构造。
func ragEngineConfig(conf *config.Config) raginfra.Config {
	return raginfra.Config{
		EmbeddingModel: conf.RagEmbeddingModel, BaseURL: conf.RagBaseUrl, APIKey: conf.RagAPIKey,
		Dimension: conf.RagDimension, ChunkSize: conf.RagChunkSize, ChunkOverlap: conf.RagChunkOverlap,
		TopK: conf.RagTopK, MaxDistance: conf.RagMaxDistance, RecallTopK: conf.RagRecallTopK,
		RerankTopK: conf.RagRerankTopK, RerankEnable: conf.RagRerankEnable,
		RerankMinScore: conf.RagRerankMinScore, EnableSemanticChunking: conf.RagEnableSemanticChunking,
		SemanticPercentile: conf.RagSemanticBreakpointPercentile, SemanticBufferSize: conf.RagSemanticBufferSize,
		ContextWindow: conf.RagContextWindow, EnableHeaderInjection: conf.RagEnableHeaderInjection,
	}
}

func ragReranker(conf *config.Config) raginfra.Reranker {
	if !conf.RagRerankEnable {
		return nil
	}
	logger.Info("rag reranker init success", "model", conf.RagRerankModel, "baseURL", conf.RagRerankBaseUrl)
	return raginfra.NewHTTPReranker(conf.RagRerankBaseUrl, conf.RagAPIKey, conf.RagRerankModel)
}
