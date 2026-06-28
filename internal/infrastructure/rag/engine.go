package rag

import (
	"context"
	"fmt"
	"strconv"

	domainrag "GopherAI/internal/domain/rag"
	redisstore "GopherAI/internal/infrastructure/cache/redis"
	"GopherAI/pkg/logger"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

// 运行期兜底默认值：配置非法（<=0）时使用。
const (
	defaultTopK        = 5
	defaultMaxDistance = 0.6
)

// Config 描述 RAG 引擎所需的配置。
type Config struct {
	// EmbeddingModel 向量嵌入模型名称。
	EmbeddingModel string
	// BaseURL 嵌入/对话 API 基础地址。
	BaseURL string
	// APIKey 嵌入 API Key，由统一配置注入。
	APIKey string
	// Dimension 向量维度。
	Dimension int
	// ChunkSize 单个文本块最大字符数。
	ChunkSize int
	// ChunkOverlap 相邻文本块重叠字符数。
	ChunkOverlap int
	// TopK 检索返回的最相关文本块数量。
	TopK int
	// MaxDistance 召回结果允许的最大向量距离（COSINE，越小越相关）。
	MaxDistance float64
}

// Engine 是 RAG 能力的统一实现：建立/删除索引、检索文档、构造提示词。
// 它实现 domain/rag.Indexer 端口，同时向 AI 层提供检索能力。
//
// embedder 在构造时创建并缓存，索引端与检索端共用，避免每次请求重建底层客户端。
type Engine struct {
	cfg      Config
	vs       *redisstore.VectorStore
	embedder embedding.Embedder
}

// NewEngine 创建 RAG 引擎，并在此一次性初始化向量嵌入器。
//
// 同时对分块、TopK、距离阈值等配置填充兜底默认值，保证后续逻辑稳定。
func NewEngine(ctx context.Context, cfg Config, vs *redisstore.VectorStore) (*Engine, error) {
	cfg = normalizeConfig(cfg)
	emb, err := newEmbedder(ctx, cfg)
	if err != nil {
		logger.Error("NewEngine create embedder failed", "err", err)
		return nil, err
	}
	logger.Info("NewEngine success",
		"embeddingModel", cfg.EmbeddingModel,
		"chunkSize", cfg.ChunkSize,
		"chunkOverlap", cfg.ChunkOverlap,
		"topK", cfg.TopK,
		"maxDistance", cfg.MaxDistance)
	return &Engine{cfg: cfg, vs: vs, embedder: emb}, nil
}

// normalizeConfig 为非法配置填充兜底默认值。
func normalizeConfig(cfg Config) Config {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = defaultChunkSize
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = defaultChunkOverlap
	}
	if cfg.TopK <= 0 {
		cfg.TopK = defaultTopK
	}
	if cfg.MaxDistance <= 0 {
		cfg.MaxDistance = defaultMaxDistance
	}
	return cfg
}

// 编译期断言：Engine 必须满足领域索引端口。
var _ domainrag.Indexer = (*Engine)(nil)

// newEmbedder 创建向量生成器；API Key 由统一配置注入。
func newEmbedder(ctx context.Context, cfg Config) (embedding.Embedder, error) {
	emb, err := embeddingArk.NewEmbedder(ctx, &embeddingArk.EmbeddingConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.EmbeddingModel,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}
	return emb, nil
}

// Index 读取并向量化指定文档，写入该账号的向量库（实现 domain/rag.Indexer）。
//
// 同一账号共用一个索引；文档被切块后，每个块以 storedName:chunk_N 作为相对 key，
// 加上账号级前缀后形成最终向量 key，便于按文档粒度删除。
func (e *Engine) Index(ctx context.Context, accountNo, storedName, localPath string) error {
	if err := e.vs.InitIndex(ctx, accountNo, e.cfg.Dimension); err != nil {
		return fmt.Errorf("failed to init redis index: %w", err)
	}

	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    e.vs.Client(),
		KeyPrefix: e.vs.AccountPrefix(accountNo),
		BatchSize: 10,
		// 定义如何把Eino的Document转换为Redis的Hash。
		DocumentToHashes: func(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {
			source := ""
			if s, ok := doc.MetaData["source"].(string); ok {
				source = s
			}
			return &redisIndexer.Hashes{
				// 最终 key = KeyPrefix + Key = rag_docs:{accountNo}:{storedName}:{chunk_N}
				Key: fmt.Sprintf("%s:%s", storedName, doc.ID),
				Field2Value: map[string]redisIndexer.FieldValue{
					"content":  {Value: doc.Content, EmbedKey: "vector"},
					"metadata": {Value: source},
				},
			}, nil
		},
	}
	indexerConfig.Embedding = e.embedder

	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	docs, err := LoadDocuments(ctx, localPath, e.cfg.ChunkSize, e.cfg.ChunkOverlap)
	if err != nil {
		return err
	}
	if _, err := idx.Store(ctx, docs); err != nil {
		return fmt.Errorf("failed to store document: %w", err)
	}
	logger.Info("Index stored", "accountNo", accountNo, "storedName", storedName, "chunks", len(docs))
	return nil
}

// Delete 删除指定账号下某个文档对应的向量数据（实现 domain/rag.Indexer）。
func (e *Engine) Delete(ctx context.Context, accountNo, storedName string) error {
	if err := e.vs.DeleteDocVectors(ctx, accountNo, storedName); err != nil {
		return fmt.Errorf("failed to delete doc vectors: %w", err)
	}
	return nil
}

// DeleteAll 删除指定账号的整个向量索引（清空知识库）。
func (e *Engine) DeleteAll(ctx context.Context, accountNo string) error {
	if err := e.vs.DeleteIndex(ctx, accountNo); err != nil {
		return fmt.Errorf("failed to delete index: %w", err)
	}
	return nil
}

// Retrieve 基于账号知识库检索与 query 最相关的文档块，返回拼装后的提示词。
//
// 返回值 hasContext 表示是否存在通过距离阈值过滤的相关内容：
//   - hasContext=true：prompt 为带参考文档的增强提示词；
//   - hasContext=false：无相关内容（无索引/召回为空/全部超阈值），调用方应回退到原始查询。
//
// 任何错误均非致命，调用方应回退到原始查询。
func (e *Engine) Retrieve(ctx context.Context, accountNo, query string) (prompt string, hasContext bool, err error) {
	retrieverConfig := &redisRetriever.RetrieverConfig{
		Client:       e.vs.Client(),
		Index:        e.vs.IndexName(accountNo),
		Dialect:      2,
		ReturnFields: []string{"content", "metadata", "distance"},
		TopK:         e.cfg.TopK,
		VectorField:  "vector",
		DocumentConverter: func(ctx context.Context, doc redisCli.Document) (*schema.Document, error) {
			resp := &schema.Document{ID: doc.ID, Content: "", MetaData: map[string]any{}}
			for field, val := range doc.Fields {
				switch field {
				case "content":
					resp.Content = val
				case "metadata":
					resp.MetaData["source"] = val
				default:
					resp.MetaData[field] = val
				}
			}
			return resp, nil
		},
	}
	retrieverConfig.Embedding = e.embedder

	rtr, err := redisRetriever.NewRetriever(ctx, retrieverConfig)
	if err != nil {
		return "", false, fmt.Errorf("failed to create retriever: %w", err)
	}

	// 真正执行检索
	docs, err := rtr.Retrieve(ctx, query)
	if err != nil {
		return "", false, fmt.Errorf("failed to retrieve documents: %w", err)
	}

	// 按距离阈值过滤不相关结果（RAG 路由的基础：过滤后为空则不注入上下文）。
	// 这里用的是cosine距离，距离越小越相关。
	relevant := FilterByDistance(docs, e.cfg.MaxDistance)
	if len(relevant) == 0 {
		logger.Info("Retrieve no relevant docs", "accountNo", accountNo, "retrieved", len(docs))
		return query, false, nil
	}
	logger.Info("Retrieve success", "accountNo", accountNo, "retrieved", len(docs), "relevant", len(relevant))
	return BuildPrompt(query, relevant), true, nil
}

// FilterByDistance 丢弃向量距离大于阈值的文档块。
// 距离字段缺失或无法解析时，保守保留该结果（避免误丢）。
func FilterByDistance(docs []*schema.Document, maxDistance float64) []*schema.Document {
	filtered := make([]*schema.Document, 0, len(docs))
	for _, d := range docs {
		dist, ok := ParseDistance(d.MetaData["distance"])
		if ok && dist > maxDistance {
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered
}

// ParseDistance 将检索结果中的 distance 字段解析为 float64。
func ParseDistance(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
