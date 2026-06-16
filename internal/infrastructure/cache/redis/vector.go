package redis

import (
	"context"
	"fmt"
	"strings"

	"GopherAI/pkg/logger"

	redisCli "github.com/redis/go-redis/v9"
)

// 向量索引的 key 命名模板。代码级约定，集中定义。
const (
	indexNameTmpl   = "rag_docs:%s:idx"
	indexPrefixTmpl = "rag_docs:%s:"
)

// VectorStore 封装 RAG 向量索引在 Redis（RediSearch）中的底层操作。
// 供 infrastructure/rag 在建立索引器/检索器时复用同一 Redis 客户端与命名约定。
type VectorStore struct {
	client *redisCli.Client
}

// NewVectorStore 创建向量索引存储。
func NewVectorStore(client *redisCli.Client) *VectorStore {
	return &VectorStore{client: client}
}

// Client 返回底层 Redis 客户端，供 eino redis 索引器/检索器使用。
func (v *VectorStore) Client() *redisCli.Client { return v.client }

// IndexName 生成向量索引名称。
func (v *VectorStore) IndexName(filename string) string {
	return fmt.Sprintf(indexNameTmpl, filename)
}

// IndexPrefix 生成向量 key 前缀。
func (v *VectorStore) IndexPrefix(filename string) string {
	return fmt.Sprintf(indexPrefixTmpl, filename)
}

// InitIndex 为指定文件创建 RediSearch 向量索引（FLAT + COSINE + FLOAT32）。
// 索引已存在时跳过（幂等）。
func (v *VectorStore) InitIndex(ctx context.Context, filename string, dimension int) error {
	indexName := v.IndexName(filename)

	// 先检查索引是否已存在。
	_, err := v.client.Do(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		logger.Info("vector index already exists, skip create", "index", indexName)
		return nil
	}
	if !strings.Contains(err.Error(), "Unknown index name") {
		return fmt.Errorf("检查索引失败: %w", err)
	}

	logger.Info("creating vector index", "index", indexName)
	prefix := v.IndexPrefix(filename)
	createArgs := []interface{}{
		"FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"SCHEMA",
		"content", "TEXT",
		"metadata", "TEXT",
		"vector", "VECTOR", "FLAT",
		"6",
		"TYPE", "FLOAT32",
		"DIM", dimension,
		"DISTANCE_METRIC", "COSINE",
	}
	if err := v.client.Do(ctx, createArgs...).Err(); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}
	logger.Info("vector index created", "index", indexName)
	return nil
}

// DeleteIndex 删除指定文件的向量索引及其关联向量数据。
func (v *VectorStore) DeleteIndex(ctx context.Context, filename string) error {
	indexName := v.IndexName(filename)
	if err := v.client.Do(ctx, "FT.DROPINDEX", indexName).Err(); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}
	logger.Info("vector index deleted", "index", indexName)
	return nil
}
