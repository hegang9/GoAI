package rag

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	domainrag "GopherAI/internal/domain/rag"
	redisstore "GopherAI/internal/infrastructure/cache/redis"
	"GopherAI/pkg/logger"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	embeddingOpenAI "github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

// 运行期兜底默认值：配置非法（<=0）时使用。
const (
	defaultTopK        = 5
	defaultMaxDistance = 0.6
	// defaultRecallTopK 启用精排时召回候选数（粗排）的兜底默认值。
	// 取较大值放大候选集，让真正相关但向量排名靠后的块有机会进入精排。
	defaultRecallTopK = 20
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
	// TopK 检索返回的最相关文本块数量（未启用精排时即最终返回数）。
	TopK int
	// MaxDistance 召回结果允许的最大向量距离（COSINE，越小越相关）。
	MaxDistance float64
	// RecallTopK 启用精排时的召回候选数（粗排放大），<=0 时运行时默认 20。
	RecallTopK int
	// RerankTopK 精排后保留的文档数，<=0 时沿用 TopK。
	RerankTopK int
	// RerankEnable 是否启用精排环节。
	RerankEnable bool
	// RerankMinScore 精排最低相关分阈值，<=0 表示不按分数过滤。
	RerankMinScore float64
	// EnableSemanticChunking 是否启用语义切分（句向量相似度断点）；关闭走递归/标题切分。
	EnableSemanticChunking bool
	// SemanticPercentile 语义断点距离分位数阈值（0-100）；<=0 时运行时默认 95。
	SemanticPercentile float64
	// SemanticBufferSize 句向量滑窗每侧大小；<0 时运行时默认 1。
	SemanticBufferSize int
	// ContextWindow 上下文增强：命中块前后各取 N 个邻居块拼接；0=关闭。
	ContextWindow int
	// EnableHeaderInjection 是否在块正文首部注入「来源｜章节」块头标签。
	EnableHeaderInjection bool
}

// Engine 是 RAG 能力的统一实现：建立/删除索引、检索文档、构造提示词。
// 它实现 domain/rag.Indexer 端口，同时向 AI 层提供检索能力。
//
// embedder 在构造时创建并缓存，索引端与检索端共用，避免每次请求重建底层客户端。
type Engine struct {
	cfg      Config
	vs       *redisstore.VectorStore
	embedder embedding.Embedder
	// reranker 精排器，可为 nil（未启用或未注入时按向量排序）。
	reranker Reranker
}

// NewEngine 创建 RAG 引擎，并在此一次性初始化向量嵌入器。
//
// reranker 为精排器，可传 nil（未启用精排时）；启用精排但传入 nil 时自动降级为向量排序。
// 同时对分块、TopK、距离阈值、召回放大等配置填充兜底默认值，保证后续逻辑稳定。
func NewEngine(ctx context.Context, cfg Config, vs *redisstore.VectorStore, reranker Reranker) (*Engine, error) {
	cfg = normalizeConfig(cfg)
	emb, err := newEmbedder(ctx, cfg)
	if err != nil {
		logger.Error("NewEngine create embedder failed", "err", err)
		return nil, err
	}
	// 配置声明启用精排但未注入实现时，明确告警并按向量排序运行，避免静默误判。
	rerankActive := cfg.RerankEnable && reranker != nil
	if cfg.RerankEnable && reranker == nil {
		logger.Warn("NewEngine rerank enabled but no reranker injected, fallback to vector order")
	}
	logger.Info("NewEngine success",
		"embeddingModel", cfg.EmbeddingModel,
		"chunkSize", cfg.ChunkSize,
		"chunkOverlap", cfg.ChunkOverlap,
		"topK", cfg.TopK,
		"maxDistance", cfg.MaxDistance,
		"rerankEnable", cfg.RerankEnable,
		"rerankActive", rerankActive,
		"recallTopK", cfg.RecallTopK,
		"rerankTopK", cfg.RerankTopK,
		"rerankMinScore", cfg.RerankMinScore,
		"enableSemanticChunking", cfg.EnableSemanticChunking,
		"semanticPercentile", cfg.SemanticPercentile,
		"semanticBufferSize", cfg.SemanticBufferSize,
		"contextWindow", cfg.ContextWindow,
		"enableHeaderInjection", cfg.EnableHeaderInjection)
	return &Engine{cfg: cfg, vs: vs, embedder: emb, reranker: reranker}, nil
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
	// 召回候选数兜底；并保证不小于 TopK，避免精排可选集反而比最终保留数还少。
	if cfg.RecallTopK <= 0 {
		cfg.RecallTopK = defaultRecallTopK
	}
	if cfg.RecallTopK < cfg.TopK {
		cfg.RecallTopK = cfg.TopK
	}
	// 精排保留数兜底：沿用 TopK。
	if cfg.RerankTopK <= 0 {
		cfg.RerankTopK = cfg.TopK
	}
	// 语义断点分位数兜底：非法（<=0 或 >100）时回到默认分位数。
	if cfg.SemanticPercentile <= 0 || cfg.SemanticPercentile > 100 {
		cfg.SemanticPercentile = defaultSemanticPercentile
	}
	// 句向量滑窗大小兜底：负数无意义，回到默认每侧 1 句。
	if cfg.SemanticBufferSize < 0 {
		cfg.SemanticBufferSize = defaultSemanticBufferSize
	}
	// 上下文窗口兜底：负数等同关闭。
	if cfg.ContextWindow < 0 {
		cfg.ContextWindow = 0
	}
	return cfg
}

// 编译期断言：Engine 必须满足领域索引端口。
var _ domainrag.Indexer = (*Engine)(nil)

// newEmbedder 创建向量生成器；API Key 由统一配置注入。
func newEmbedder(ctx context.Context, cfg Config) (embedding.Embedder, error) {
	// SiliconFlow 兼容 OpenAI Embeddings，并支持通过 dimensions 指定输出维度。
	if strings.Contains(strings.ToLower(cfg.BaseURL), "siliconflow.cn") {
		dimensions := cfg.Dimension
		emb, err := embeddingOpenAI.NewEmbeddingClient(ctx, &embeddingOpenAI.EmbeddingConfig{
			BaseURL:    cfg.BaseURL,
			APIKey:     cfg.APIKey,
			Model:      cfg.EmbeddingModel,
			Dimensions: &dimensions,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create SiliconFlow embedder: %w", err)
		}
		return emb, nil
	}

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
	logger.Info("Index load documents start",
		"accountNo", accountNo,
		"storedName", storedName,
		"localPath", localPath,
		"enableSemanticChunking", e.cfg.EnableSemanticChunking,
		"semanticPercentile", e.cfg.SemanticPercentile,
		"semanticBufferSize", e.cfg.SemanticBufferSize,
		"enableHeaderInjection", e.cfg.EnableHeaderInjection,
		"contextWindow", e.cfg.ContextWindow)

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
			// 上下文增强定位元数据：额外写入 chunk（序号）与 stored（storedName）两个普通 HASH 字段，
			// 检索期据此确定性拼接邻居块 key，避免脆弱的 key 解析；存量旧文档无此字段时不触发扩展。
			f2v := map[string]redisIndexer.FieldValue{
				"content":  {Value: doc.Content, EmbedKey: "vector"},
				"metadata": {Value: source},
				"stored":   {Value: storedName},
			}
			if c, ok := doc.MetaData["chunk"].(int); ok {
				f2v["chunk"] = redisIndexer.FieldValue{Value: strconv.Itoa(c)}
			}
			// 块头标签（可选）：注入开启时写入章节路径，供检索期引用展示与可选过滤。
			if h, ok := doc.MetaData["headers"].(string); ok && h != "" {
				f2v["headers"] = redisIndexer.FieldValue{Value: h}
			}
			return &redisIndexer.Hashes{
				// 最终 key = KeyPrefix + Key = rag_docs:{accountNo}:{storedName}:{chunk_N}
				Key:         fmt.Sprintf("%s:%s", storedName, doc.ID),
				Field2Value: f2v,
			}, nil
		},
	}
	indexerConfig.Embedding = e.embedder

	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	// 按引擎配置透传分块升级开关（语义切分 / 块头标签）；Embedder 复用引擎内缓存实例。
	docs, err := loadDocuments(ctx, localPath, loadOptions{
		ChunkSize:              e.cfg.ChunkSize,
		Overlap:                e.cfg.ChunkOverlap,
		Embedder:               e.embedder,
		EnableSemanticChunking: e.cfg.EnableSemanticChunking,
		SemanticPercentile:     e.cfg.SemanticPercentile,
		SemanticBufferSize:     e.cfg.SemanticBufferSize,
		EnableHeaderInjection:  e.cfg.EnableHeaderInjection,
	})
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

// RetrieveFilter 描述检索期的元数据过滤范围，用于缩小检索范围。
//
// 两个字段可同时设置（AND 关系），也可都为空（不过滤，行为与改动前完全一致），
// 由调用方（如 ai/rag.go）根据用户意图或请求参数构造。
type RetrieveFilter struct {
	// StoredName 限定只检索某个来源文档；为空时不限来源。
	// 对应索引中 stored TAG 字段，走 @stored:{name} 精确匹配。
	StoredName string
	// Headers 限定章节路径关键字；为空时不限章节。
	// 对应索引中 headers TEXT 字段，走 @headers:keyword 模糊匹配。
	Headers string
}

// toFilterExpr 把过滤参数转换为 RediSearch 的 FILTER 查询表达式。
//
// 多个条件之间用空格连接（RediSearch 中空格 = AND）；
// 为空时返回空串，调用方据此跳过 WithFilterQuery，避免传空过滤表达式。
func (f RetrieveFilter) toFilterExpr() string {
	var parts []string
	if f.StoredName != "" {
		// stored 是 TAG 字段，用 @{field}:{value} 语法精确匹配；
		// TAG 值含特殊字符会破坏查询语法，故先经 escapeTag 转义。
		parts = append(parts, fmt.Sprintf("@stored:{%s}", escapeTag(f.StoredName)))
	}
	if f.Headers != "" {
		// headers 是 TEXT 字段，用 @{field}:keyword 语法模糊匹配章节路径；
		// TEXT 查询保留字符（:(){}[]"'^~* 等）会改变查询语义，需经 escapeText 转义。
		parts = append(parts, fmt.Sprintf("@headers:%s", escapeText(f.Headers)))
	}
	return strings.Join(parts, " ")
}

// rediSearchTagSpecialChars 是 TAG 值中需要反斜杠转义的保留字符集合。
// 这些字符在 RediSearch TAG 查询里有语法含义（如 {} 包围值、, 分隔多值），
// 若原样出现会破坏查询语法或造成注入风险。
const rediSearchTagSpecialChars = `,.<>{}[]"':;!@#$%^&*()-+=~`

// escapeTag 转义 TAG 字段值，使其可安全地放入 @{field}:{value} 表达式。
func escapeTag(s string) string {
	return escapeChars(s, rediSearchTagSpecialChars)
}

// rediSearchTextSpecialChars 是 TEXT 全文查询中需要转义的保留字符集合。
// 这些字符在 RediSearch 查询语法中有特殊含义（如 : 分隔字段与值、* 通配、() 分组），
// 章节路径中一般不含这些字符，转义是为防御异常输入。
const rediSearchTextSpecialChars = `:.,<>(){}[]"'^~*+-=!@#$%&|/\`

// escapeText 转义 TEXT 字段查询值，使其作为字面量参与全文匹配而非被当作查询语法。
func escapeText(s string) string {
	return escapeChars(s, rediSearchTextSpecialChars)
}

// escapeChars 对 s 中所有出现在 specialChars 中的字符加反斜杠前缀。
// 使用 strings.Builder 避免高频字符串拼接的开销。
func escapeChars(s, specialChars string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(specialChars, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Retrieve 基于账号知识库检索与 query 最相关的文档块，返回拼装后的提示词。
//
// 两阶段检索：向量召回（粗排，启用精排时放大到 RecallTopK）→ 距离粗筛
// → reranker 精排打分并截断到 RerankTopK →（可选）最低分阈值过滤 → BuildPrompt。
//
// 返回值 hasContext 表示是否存在通过过滤的相关内容：
//   - hasContext=true：prompt 为带参考文档的增强提示词；
//   - hasContext=false：无相关内容（无索引/召回为空/全部超阈值），调用方应回退到原始查询。
//
// 任何错误均非致命，调用方应回退到原始查询；精排失败时自动降级为向量排序，不中断链路。
//
// 参数 f 为元数据过滤范围；传入零值 RetrieveFilter{} 表示不过滤，行为与改动前一致。
// 过滤条件经 toFilterExpr 转换为 RediSearch FILTER 子句，在向量召回阶段就生效（pre-filter），
// 缩小检索范围后再参与距离粗筛与精排。
// DocScore 是带评分的召回块，供评测观测各阶段排序与分数。
//
// 字段从 schema.Document 元数据提取，距离/精排分缺失时填 -1 表示「无此分数」。
type DocScore struct {
	// Content 块正文。
	Content string
	// StoredName 来源文档名，来自 metadata["stored"]。
	StoredName string
	// Headers 章节路径关键字，来自 metadata["headers"]。
	Headers string
	// Distance 粗排向量距离（COSINE，越小越相关），无则 -1。
	Distance float64
	// RerankScore 精排相关分（越大越相关），无则 -1。
	RerankScore float64
}

// RetrieveDetail 是检索四阶段的中间结果，供评测脚本判断命中质量与精排增益。
//
// 阶段语义：
//   - Retrieved：粗排召回（含距离）
//   - Relevant：距离粗筛后
//   - Reranked：精排后（未启用精排则同 Relevant）
//   - Final：邻居扩展后，实际拼进 prompt 的
type RetrieveDetail struct {
	Retrieved []DocScore
	Relevant  []DocScore
	Reranked  []DocScore
	Final     []DocScore
}

// RetrieveDetail 执行检索并返回四阶段完整明细，供评测脚本判断命中质量。
//
// 与 Retrieve 共用主体逻辑，只是不拼 prompt、把粗排/粗筛/精排/邻居扩展的中间结果吐出。
// 未启用精排时 Reranked 与 Relevant 一致；ContextWindow=0 时 Final 与 Reranked 一致。
func (e *Engine) RetrieveDetail(ctx context.Context, accountNo, query string, f RetrieveFilter) (RetrieveDetail, error) {
	// 是否本次实际启用精排：需同时满足配置开启与注入了精排器。
	rerankActive := e.cfg.RerankEnable && e.reranker != nil

	// 召回阶段：未启用精排时取 TopK；启用时放大到 RecallTopK 以提供更充足的精排候选。
	recallTopK := e.cfg.TopK
	if rerankActive {
		recallTopK = e.cfg.RecallTopK
	}

	retrieverConfig := &redisRetriever.RetrieverConfig{
		Client:       e.vs.Client(),
		Index:        e.vs.IndexName(accountNo),
		Dialect:      2,
		ReturnFields: []string{"content", "metadata", "distance", "chunk", "stored", "headers"},
		TopK:         recallTopK,
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
		return RetrieveDetail{}, fmt.Errorf("failed to create retriever: %w", err)
	}

	// 构造检索选项：WithFilterQuery 是 eino-ext redis retriever 提供的过滤选项，
	// 其返回类型为 retriever.Option（eino 通用检索器选项），而非 redisRetriever 自有类型。
	// 仅在存在过滤表达式时附加，避免传空串导致查询语义异常。
	opts := []retriever.Option{}
	if expr := f.toFilterExpr(); expr != "" {
		opts = append(opts, redisRetriever.WithFilterQuery(expr))
	}

	// 真正执行检索（过滤条件通过 opts 透传给底层 FT.SEARCH 的 FILTER 子句）
	docs, err := rtr.Retrieve(ctx, query, opts...)
	if err != nil {
		return RetrieveDetail{}, fmt.Errorf("failed to retrieve documents: %w", err)
	}

	// 粗筛：按距离阈值丢掉明显不相关的候选（RAG 路由的基础：为空则不注入上下文）。
	// 这里用的是 cosine 距离，距离越小越相关。
	relevant := FilterByDistance(docs, e.cfg.MaxDistance)

	// 记录粗筛后、精排前的顺序，供 Reranked 对比精排增益。
	relevantBeforeRerank := append([]*schema.Document(nil), relevant...)

	// 精排阶段：对粗筛后的候选用 reranker 重新打分排序并截断；
	// 失败时记录告警并降级为向量排序，保证 RAG 链路不中断。
	if rerankActive && len(relevant) > 0 {
		topN := e.cfg.RerankTopK
		if topN <= 0 {
			topN = e.cfg.TopK
		}
		// 精排
		reranked, rerr := e.reranker.Rerank(ctx, query, relevant, topN)
		if rerr != nil {
			logger.Warn("rerank failed, fallback to vector order", "accountNo", accountNo, "err", rerr)
		} else {
			// 精排成功后再按最低相关分阈值兜底过滤（阈值<=0 时不过滤）。
			relevant = FilterByRerankScore(reranked, e.cfg.RerankMinScore)
		}
	}

	if len(relevant) == 0 {
		logger.Info("Retrieve no relevant docs", "accountNo", accountNo, "retrieved", len(docs))
		// 仍返回粗排召回明细，便于评测观测「召回了但全被粗筛/精排过滤」的场景。
		return RetrieveDetail{
			Retrieved: toDocScores(docs),
			Relevant:  nil,
			Reranked:  nil,
			Final:     nil,
		}, nil
	}

	// 上下文增强（small-to-big / 句窗）：检索打分仍用小块保精度，命中后按确定性 key
	// 取回命中块前后各 ContextWindow 个邻居块拼接，兼顾召回精度与上下文完整性；
	// ContextWindow=0（默认）时此步为空操作，存量无定位元数据的旧块自动跳过（优雅降级）。
	if e.cfg.ContextWindow > 0 {
		logger.Info("context window expansion start",
			"accountNo", accountNo,
			"window", e.cfg.ContextWindow,
			"relevant", len(relevant))
		var neighborFetches, neighborMisses, neighborErrors int
		fetch := func(stored string, idx int) string {
			neighborFetches++
			c, ferr := e.vs.GetNeighborChunk(ctx, accountNo, stored, idx)
			if ferr != nil {
				neighborErrors++
				logger.Warn("fetch neighbor chunk failed",
					"accountNo", accountNo, "stored", stored, "chunk", idx, "err", ferr)
				return ""
			}
			if c == "" {
				neighborMisses++
			}
			return c
		}
		before := len(relevant)
		relevant = expandWithNeighbors(relevant, e.cfg.ContextWindow, fetch)
		logger.Info("context window expanded",
			"accountNo", accountNo,
			"window", e.cfg.ContextWindow,
			"before", before,
			"after", len(relevant),
			"neighborFetches", neighborFetches,
			"neighborMisses", neighborMisses,
			"neighborErrors", neighborErrors)
	}

	logger.Info("Retrieve success",
		"accountNo", accountNo,
		"retrieved", len(docs),
		"relevant", len(relevant),
		"rerankActive", rerankActive)

	// Reranked 记录精排后的顺序：
	//   - 启用精排：relevant 此时已是精排+分数过滤后的结果
	//   - 未启用精排：与粗筛后顺序一致（relevantBeforeRerank）
	reranked := relevantBeforeRerank
	if rerankActive {
		reranked = relevant
	}

	return RetrieveDetail{
		Retrieved: toDocScores(docs),
		Relevant:  toDocScores(relevantBeforeRerank),
		Reranked:  toDocScores(reranked),
		Final:     toDocScores(relevant),
	}, nil
}

// toDocScores 把 schema.Document 列表转为带评分的 DocScore 列表，提取 stored/headers/distance/rerank_score。
func toDocScores(docs []*schema.Document) []DocScore {
	out := make([]DocScore, 0, len(docs))
	for _, d := range docs {
		ds := DocScore{Content: d.Content, Distance: -1, RerankScore: -1}
		if d.MetaData != nil {
			if s, ok := d.MetaData["stored"].(string); ok {
				ds.StoredName = s
			}
			if h, ok := d.MetaData["headers"].(string); ok {
				ds.Headers = h
			}
			if dist, ok := ParseDistance(d.MetaData["distance"]); ok {
				ds.Distance = dist
			}
			if rs, ok := d.MetaData["rerank_score"].(float64); ok {
				ds.RerankScore = rs
			}
		}
		out = append(out, ds)
	}
	return out
}

// toDocuments 把 DocScore 列表转回 schema.Document 列表，供 BuildPrompt 拼装 prompt。
func toDocuments(scores []DocScore) []*schema.Document {
	out := make([]*schema.Document, 0, len(scores))
	for _, s := range scores {
		out = append(out, &schema.Document{Content: s.Content})
	}
	return out
}

// Retrieve 执行检索并构造增强 prompt。
//
// 返回值：
//   - prompt：可直接替换最后一条用户消息的增强 prompt；无命中时回退为原 query
//   - hasContext：是否命中相关文档，false 时调用方应透传原消息
//   - hitCount：命中并参与拼装的文档块数（粗筛+精排+邻居扩展后的最终块数），供观测用
//   - err：检索链路错误
//
// 内部复用 RetrieveDetail，行为与重构前一致。
func (e *Engine) Retrieve(ctx context.Context, accountNo, query string, f RetrieveFilter) (prompt string, hasContext bool, hitCount int, err error) {
	detail, err := e.RetrieveDetail(ctx, accountNo, query, f)
	if err != nil {
		return "", false, 0, err
	}
	if len(detail.Final) == 0 {
		return query, false, 0, nil
	}
	return BuildPrompt(query, toDocuments(detail.Final)), true, len(detail.Final), nil
}

// chunkLocator 从文档元数据解析上下文增强所需的定位信息 (storedName, chunk序号)。
//
// chunk 序号兼容索引期写入的 int 与检索期返回的 string 两种类型；
// 缺少 stored 或 chunk（存量旧文档）时返回 ok=false，调用方据此跳过扩展，实现优雅降级。
func chunkLocator(d *schema.Document) (stored string, idx int, ok bool) {
	if d == nil || d.MetaData == nil {
		return "", 0, false
	}
	s, has := d.MetaData["stored"].(string)
	if !has || s == "" {
		return "", 0, false
	}
	switch c := d.MetaData["chunk"].(type) {
	case int:
		return s, c, true
	case int64:
		return s, int(c), true
	case float64:
		return s, int(c), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(c))
		if err != nil {
			return "", 0, false
		}
		return s, n, true
	default:
		return "", 0, false
	}
}

// expandWithNeighbors 按 window 把每个命中块扩展为「前后各 window 个邻居块」的连续 span，
// 并跨命中去重、合并相邻 span，提升交给模型的上下文完整性。
//
// 行为约定：
//   - window<=0 时原样返回，不做任何扩展；
//   - 无法定位的块（存量旧文档缺定位元数据）原样保留；
//   - 命中块用自身正文，邻居块经 fetch 按 (stored, idx) 取回；fetch 返回空（越界 / 不存在）时跳过；
//   - 已被先前命中窗口覆盖的命中块去重跳过；相邻或重叠的窗口合并进同一扩展块，避免内容重复。
//
// fetch 由调用方注入（生产环境读 Redis，测试可注入桩），便于隔离存储依赖。
func expandWithNeighbors(docs []*schema.Document, window int, fetch func(stored string, idx int) string) []*schema.Document {
	if window <= 0 {
		return docs
	}
	out := make([]*schema.Document, 0, len(docs))

	// group 记录某个 stored 文档已输出的最近一个合并块及其覆盖的 chunk 闭区间，
	// 用于跨命中去重（命中落在覆盖区内则跳过）与相邻合并（窗口相接则追加到同一块）。
	type group struct {
		lo, hi int
		doc    *schema.Document
		parts  []string
	}
	last := map[string]*group{}

	for _, d := range docs {
		stored, idx, ok := chunkLocator(d)
		if !ok {
			// 无法定位（存量旧文档）→ 原样保留，不做扩展。
			out = append(out, d)
			continue
		}
		g := last[stored]
		if g != nil && idx >= g.lo && idx <= g.hi {
			// 命中块已落在上一个合并块覆盖范围内 → 去重跳过。
			continue
		}
		lo := idx - window
		if lo < 0 {
			lo = 0
		}
		hi := idx + window

		// 与上一个合并块相邻或重叠 → 合并：仅向其追加尚未覆盖的邻居块，避免重复内容。
		if g != nil && lo <= g.hi+1 && hi > g.hi {
			for i := g.hi + 1; i <= hi; i++ {
				if part := neighborPart(d, stored, i, idx, fetch); part != "" {
					g.parts = append(g.parts, part)
				}
			}
			g.hi = hi
			g.doc.Content = strings.Join(g.parts, "\n")
			continue
		}

		// 否则新建一个合并块：复制命中块（保留 source/headers 等元数据），正文换成窗口拼接结果。
		nd := *d
		ng := &group{lo: lo, hi: hi, doc: &nd}
		for i := lo; i <= hi; i++ {
			if part := neighborPart(d, stored, i, idx, fetch); part != "" {
				ng.parts = append(ng.parts, part)
			}
		}
		nd.Content = strings.Join(ng.parts, "\n")
		last[stored] = ng
		out = append(out, &nd)
	}
	return out
}

// neighborPart 取窗口内第 i 个 chunk 的文本：命中块用自身正文，邻居块经 fetch 取回；
// 纯空白（越界 / 不存在）返回空串，由调用方跳过。
func neighborPart(hit *schema.Document, stored string, i, hitIdx int, fetch func(string, int) string) string {
	var part string
	if i == hitIdx {
		part = hit.Content
	} else {
		part = fetch(stored, i)
	}
	if strings.TrimSpace(part) == "" {
		return ""
	}
	return part
}

// FilterByRerankScore 按精排最低分阈值过滤；阈值<=0 时不过滤。
// 分数字段缺失或类型不符时保守保留（避免因解析问题误丢精排结果）。
func FilterByRerankScore(docs []*schema.Document, minScore float64) []*schema.Document {
	if minScore <= 0 {
		return docs
	}
	out := make([]*schema.Document, 0, len(docs))
	for _, d := range docs {
		if s, ok := d.MetaData["rerank_score"].(float64); ok && s < minScore {
			continue
		}
		out = append(out, d)
	}
	return out
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
