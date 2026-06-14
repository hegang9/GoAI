package rag

import (
	"GopherAI/common/redis"
	"GopherAI/config"
	"context"
	"fmt"

	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

// RAGQuery 承载知识库的“检索服务”职责：基于向量相似度检索相关文档。
type RAGQuery struct {
	embedding embedding.Embedder  // 向量生成器（用于把查询语句向量化）
	retriever retriever.Retriever // Redis 向量检索器
}

// NewRAGQuery 基于已建立索引的文档名创建检索器。
//
// 该函数仅关注“检索逻辑”，不再负责定位用户上传文件——
// 文件系统约定（uploads/{account_no}）已下沉到 store.go 的 ResolveUserDocFilename，
// 调用方先解析出文件名，再传入此处构建检索器。
func NewRAGQuery(ctx context.Context, filename string) (*RAGQuery, error) {
	cfg := config.GetConfig()

	embedder, err := newEmbedder(ctx, cfg.RagModelConfig.RagEmbeddingModel)
	if err != nil {
		return nil, err
	}

	retrieverConfig := &redisRetriever.RetrieverConfig{
		Client:       redis.Rdb,
		Index:        redis.GenerateIndexName(filename),
		Dialect:      2,
		ReturnFields: []string{"content", "metadata", "distance"},
		TopK:         5,
		VectorField:  "vector",
		DocumentConverter: func(ctx context.Context, doc redisCli.Document) (*schema.Document, error) {
			resp := &schema.Document{
				ID:       doc.ID,
				Content:  "",
				MetaData: map[string]any{},
			}
			for field, val := range doc.Fields {
				if field == "content" {
					resp.Content = val
				} else {
					resp.MetaData[field] = val
				}
			}
			return resp, nil
		},
	}
	retrieverConfig.Embedding = embedder

	rtr, err := redisRetriever.NewRetriever(ctx, retrieverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create retriever: %w", err)
	}

	return &RAGQuery{embedding: embedder, retriever: rtr}, nil
}

// RetrieveDocuments 按语义检索与查询最相关的文档。
func (r *RAGQuery) RetrieveDocuments(ctx context.Context, query string) ([]*schema.Document, error) {
	docs, err := r.retriever.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve documents: %w", err)
	}
	return docs, nil
}
