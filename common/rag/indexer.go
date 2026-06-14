package rag

import (
	"GopherAI/common/redis"
	"GopherAI/config"
	"context"
	"fmt"

	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
)

// RAGIndexer 承载知识库的“向量索引”职责：
// 把文档向量化并写入 Redis，同时通过 DeleteIndex 管理索引生命周期。
type RAGIndexer struct {
	embedding embedding.Embedder    // 向量生成器
	indexer   *redisIndexer.Indexer // Redis 向量索引器
}

// NewRAGIndexer 构建知识库索引器。
//
// 流程：创建向量生成器 → 在 Redis 中初始化向量索引结构 → 配置并创建索引器。
// 通俗理解：先准备好“翻译官”，再在 Redis 里建好“仓库”，最后组装出可写入的索引器。
func NewRAGIndexer(filename, embeddingModel string) (*RAGIndexer, error) {
	// 用于控制初始化流程（超时 / 取消等），这里使用默认背景即可。
	ctx := context.Background()

	embedder, err := newEmbedder(ctx, embeddingModel)
	if err != nil {
		return nil, err
	}

	// 向量维度必须在创建索引前确定，Redis 依据它建立向量索引结构。
	dimension := config.GetConfig().RagModelConfig.RagDimension
	if err := redis.InitRedisIndex(ctx, filename, dimension); err != nil {
		return nil, fmt.Errorf("failed to init redis index: %w", err)
	}

	// 配置索引器：定义一段文档在 Redis 中如何存储。
	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    redis.Rdb,                               // Redis 客户端
		KeyPrefix: redis.GenerateIndexNamePrefix(filename), // 不同知识库使用不同前缀，避免冲突
		BatchSize: 10,                                      // 批量写入，提高效率
		DocumentToHashes: func(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {
			// 从文档元数据取出来源信息（如文件名）。
			source := ""
			if s, ok := doc.MetaData["source"].(string); ok {
				source = s
			}
			return &redisIndexer.Hashes{
				Key: fmt.Sprintf("%s:%s", filename, doc.ID),
				Field2Value: map[string]redisIndexer.FieldValue{
					// content：原始文本，EmbedKey 表示需向量化后存入 "vector" 字段。
					"content": {Value: doc.Content, EmbedKey: "vector"},
					// metadata：辅助信息，不参与向量计算。
					"metadata": {Value: source},
				},
			}, nil
		},
	}
	// 将向量生成器交给索引器，写入文本时自动完成向量计算。
	indexerConfig.Embedding = embedder

	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	return &RAGIndexer{embedding: embedder, indexer: idx}, nil
}

// IndexFile 加载文件内容并写入向量索引（向量化由索引器自动完成）。
// 文档的读取与切块逻辑委托给 LoadDocuments，本方法只负责存储。
func (r *RAGIndexer) IndexFile(ctx context.Context, filePath string) error {
	docs, err := LoadDocuments(filePath)
	if err != nil {
		return err
	}

	if _, err := r.indexer.Store(ctx, docs); err != nil {
		return fmt.Errorf("failed to store document: %w", err)
	}
	return nil
}

// DeleteIndex 删除指定文件对应的知识库索引，用于管理索引生命周期。
// 不依赖 RAGIndexer 实例，可在仅知道文件名时直接调用。
func DeleteIndex(ctx context.Context, filename string) error {
	if err := redis.DeleteRedisIndex(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete redis index: %w", err)
	}
	return nil
}
