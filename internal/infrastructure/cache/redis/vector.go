package redis

import (
	"context"
	"fmt"
	"strings"

	"GopherAI/pkg/logger"

	redisCli "github.com/redis/go-redis/v9"
)

// 向量索引的 key 命名模板。代码级约定，集中定义。
//
// 多文档知识库设计：索引按账号聚合（每账号一个 RediSearch 索引），
// 单个文档的向量 key 在账号前缀下再加 storedName 段，从而支持按文档粒度删除。
//
//	索引名：     rag_docs:{accountNo}:idx
//	账号级前缀：  rag_docs:{accountNo}:                （索引 PREFIX 与索引器 KeyPrefix）
//	文档级前缀：  rag_docs:{accountNo}:{storedName}:    （按文档删除时的匹配前缀）
//	最终向量 key：rag_docs:{accountNo}:{storedName}:chunk_N
const (
	indexNameTmpl     = "rag_docs:%s:idx"
	accountPrefixTmpl = "rag_docs:%s:"
	docKeyPrefixTmpl  = "rag_docs:%s:%s:"
)

// scanBatch 删除文档向量时 SCAN 的单批游标步长。
const scanBatch = 200

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

// IndexName 生成账号级向量索引名称。
func (v *VectorStore) IndexName(accountNo string) string {
	return fmt.Sprintf(indexNameTmpl, accountNo)
}

// AccountPrefix 生成账号级向量 key 前缀（同时用作 RediSearch 索引 PREFIX 与索引器 KeyPrefix）。
func (v *VectorStore) AccountPrefix(accountNo string) string {
	return fmt.Sprintf(accountPrefixTmpl, accountNo)
}

// DocKeyPrefix 生成单个文档的向量 key 前缀，用于按文档删除。
func (v *VectorStore) DocKeyPrefix(accountNo, storedName string) string {
	return fmt.Sprintf(docKeyPrefixTmpl, accountNo, storedName)
}

// InitIndex 为指定账号创建 RediSearch 向量索引（FLAT + COSINE + FLOAT32）。
// 索引已存在时跳过（幂等）。同一账号下的全部文档共用该索引。
func (v *VectorStore) InitIndex(ctx context.Context, accountNo string, dimension int) error {
	indexName := v.IndexName(accountNo)

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
	prefix := v.AccountPrefix(accountNo)
	// 组装创建索引命令
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

// GetNeighborChunk 按确定性 key 取回某个文档指定序号块的正文，用于检索期的上下文增强（取回邻居块）。
//
// key 由账号前缀 + storedName + chunk_N 确定性拼接（storedName 为 uuid 不含 ":"）：
//
//	rag_docs:{accountNo}:{storedName}:chunk_{idx}
//
// 块不存在（越界 / 存量旧文档）时返回空串且 err 为 nil，由调用方安全跳过（优雅降级）。
func (v *VectorStore) GetNeighborChunk(ctx context.Context, accountNo, storedName string, idx int) (string, error) {
	if idx < 0 {
		logger.Debug("neighbor chunk skipped: negative index",
			"accountNo", accountNo,
			"storedName", storedName,
			"chunk", idx)
		return "", nil
	}
	key := fmt.Sprintf("%s%s:chunk_%d", v.AccountPrefix(accountNo), storedName, idx)
	logger.Debug("neighbor chunk fetch start",
		"accountNo", accountNo,
		"storedName", storedName,
		"chunk", idx,
		"key", key)
	val, err := v.client.HGet(ctx, key, "content").Result()
	if err == redisCli.Nil {
		logger.Debug("neighbor chunk missing",
			"accountNo", accountNo,
			"storedName", storedName,
			"chunk", idx,
			"key", key)
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("取回邻居块失败: %w", err)
	}
	logger.Debug("neighbor chunk fetched",
		"accountNo", accountNo,
		"storedName", storedName,
		"chunk", idx,
		"key", key,
		"contentLen", len([]rune(val)))
	return val, nil
}

// DeleteDocVectors 删除指定账号下某个文档的全部向量数据（保留账号索引本身）。
// 通过 SCAN 匹配文档级前缀，分批删除，避免使用阻塞性的 KEYS。
func (v *VectorStore) DeleteDocVectors(ctx context.Context, accountNo, storedName string) error {
	match := v.DocKeyPrefix(accountNo, storedName) + "*"
	var cursor uint64
	var removed int
	for {
		keys, next, err := v.client.Scan(ctx, cursor, match, scanBatch).Result()
		if err != nil {
			return fmt.Errorf("扫描文档向量失败: %w", err)
		}
		if len(keys) > 0 {
			if err := v.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("删除文档向量失败: %w", err)
			}
			removed += len(keys)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	logger.Info("doc vectors deleted", "accountNo", accountNo, "storedName", storedName, "removed", removed)
	return nil
}

// DeleteIndex 删除指定账号的整个向量索引及其关联向量数据（DD 选项）。
// 用于彻底清空某账号知识库。索引不存在时视为成功。
func (v *VectorStore) DeleteIndex(ctx context.Context, accountNo string) error {
	indexName := v.IndexName(accountNo)
	if err := v.client.Do(ctx, "FT.DROPINDEX", indexName, "DD").Err(); err != nil {
		if strings.Contains(err.Error(), "Unknown index name") {
			return nil
		}
		return fmt.Errorf("删除索引失败: %w", err)
	}
	logger.Info("vector index deleted", "index", indexName)
	return nil
}
