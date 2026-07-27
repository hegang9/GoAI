package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const indexFingerprintVersion = "rag-index-v1"

// IndexConfigFingerprint 只对会改变索引内容或向量空间的配置生成稳定指纹。
// TopK、距离阈值、上下文窗口和 reranker 等检索期配置刻意不参与计算。
func IndexConfigFingerprint(cfg Config) string {
	cfg = normalizeConfig(cfg)
	payload := struct {
		Version                string  `json:"version"`
		EmbeddingModel         string  `json:"embedding_model"`
		BaseURL                string  `json:"base_url"`
		Dimension              int     `json:"dimension"`
		ChunkSize              int     `json:"chunk_size"`
		ChunkOverlap           int     `json:"chunk_overlap"`
		EnableSemanticChunking bool    `json:"enable_semantic_chunking"`
		SemanticPercentile     float64 `json:"semantic_percentile"`
		SemanticBufferSize     int     `json:"semantic_buffer_size"`
		EnableHeaderInjection  bool    `json:"enable_header_injection"`
	}{
		Version:        indexFingerprintVersion,
		EmbeddingModel: cfg.EmbeddingModel, BaseURL: cfg.BaseURL, Dimension: cfg.Dimension,
		ChunkSize: cfg.ChunkSize, ChunkOverlap: cfg.ChunkOverlap,
		EnableSemanticChunking: cfg.EnableSemanticChunking, SemanticPercentile: cfg.SemanticPercentile,
		SemanticBufferSize: cfg.SemanticBufferSize, EnableHeaderInjection: cfg.EnableHeaderInjection,
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
