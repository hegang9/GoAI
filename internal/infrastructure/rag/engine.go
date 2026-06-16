package rag

import (
	"context"
	"fmt"
	"os"

	domainrag "GopherAI/internal/domain/rag"
	redisstore "GopherAI/internal/infrastructure/cache/redis"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

// Config 描述 RAG 引擎所需的配置。
type Config struct {
	// EmbeddingModel 向量嵌入模型名称。
	EmbeddingModel string
	// BaseURL 嵌入/对话 API 基础地址。
	BaseURL string
	// Dimension 向量维度。
	Dimension int
}

// Engine 是 RAG 能力的统一实现：建立/删除索引、检索文档、构造提示词。
// 它实现 domain/rag.Indexer 端口，同时向 AI 层提供检索能力。
type Engine struct {
	cfg Config
	vs  *redisstore.VectorStore
}

// NewEngine 创建 RAG 引擎。
func NewEngine(cfg Config, vs *redisstore.VectorStore) *Engine {
	return &Engine{cfg: cfg, vs: vs}
}

// 编译期断言：Engine 必须满足领域索引端口。
var _ domainrag.Indexer = (*Engine)(nil)

// embedder 创建向量生成器，索引端与检索端共用。
func (e *Engine) embedder(ctx context.Context, model string) (embedding.Embedder, error) {
	// 向量模型 API Key 沿用环境变量读取方式。
	apiKey := os.Getenv("OPENAI_API_KEY")
	embedder, err := embeddingArk.NewEmbedder(ctx, &embeddingArk.EmbeddingConfig{
		BaseURL: e.cfg.BaseURL,
		APIKey:  apiKey,
		Model:   model,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}
	return embedder, nil
}

// Index 读取并向量化指定文档，写入向量库（实现 domain/rag.Indexer）。
func (e *Engine) Index(ctx context.Context, storedName, localPath string) error {
	embedder, err := e.embedder(ctx, e.cfg.EmbeddingModel)
	if err != nil {
		return err
	}

	if err := e.vs.InitIndex(ctx, storedName, e.cfg.Dimension); err != nil {
		return fmt.Errorf("failed to init redis index: %w", err)
	}

	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    e.vs.Client(),
		KeyPrefix: e.vs.IndexPrefix(storedName),
		BatchSize: 10,
		DocumentToHashes: func(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {
			source := ""
			if s, ok := doc.MetaData["source"].(string); ok {
				source = s
			}
			return &redisIndexer.Hashes{
				Key: fmt.Sprintf("%s:%s", storedName, doc.ID),
				Field2Value: map[string]redisIndexer.FieldValue{
					"content":  {Value: doc.Content, EmbedKey: "vector"},
					"metadata": {Value: source},
				},
			}, nil
		},
	}
	indexerConfig.Embedding = embedder

	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	docs, err := loadDocuments(localPath)
	if err != nil {
		return err
	}
	if _, err := idx.Store(ctx, docs); err != nil {
		return fmt.Errorf("failed to store document: %w", err)
	}
	return nil
}

// Delete 删除指定文档对应的向量索引（实现 domain/rag.Indexer）。
func (e *Engine) Delete(ctx context.Context, storedName string) error {
	if err := e.vs.DeleteIndex(ctx, storedName); err != nil {
		return fmt.Errorf("failed to delete redis index: %w", err)
	}
	return nil
}

// Retrieve 基于已建立索引的文档名检索与 query 最相关的文档，并返回拼装后的提示词。
// 若任何环节失败，调用方应回退到原始查询（不视为致命错误）。
func (e *Engine) Retrieve(ctx context.Context, storedName, query string) (string, error) {
	embedder, err := e.embedder(ctx, e.cfg.EmbeddingModel)
	if err != nil {
		return "", err
	}

	retrieverConfig := &redisRetriever.RetrieverConfig{
		Client:       e.vs.Client(),
		Index:        e.vs.IndexName(storedName),
		Dialect:      2,
		ReturnFields: []string{"content", "metadata", "distance"},
		TopK:         5,
		VectorField:  "vector",
		DocumentConverter: func(ctx context.Context, doc redisCli.Document) (*schema.Document, error) {
			resp := &schema.Document{ID: doc.ID, Content: "", MetaData: map[string]any{}}
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
		return "", fmt.Errorf("failed to create retriever: %w", err)
	}

	docs, err := rtr.Retrieve(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve documents: %w", err)
	}
	return buildPrompt(query, docs), nil
}
