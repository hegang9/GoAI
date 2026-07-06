// Package rag 是检索增强适配层：基于 eino + Redis 向量库实现 domain/rag.Indexer 端口，
// 并向 infrastructure/ai 提供检索与提示词构造能力。
package rag

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"GopherAI/pkg/logger"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
)

// 分块参数兜底默认值：当配置非法（<=0）时在分块阶段使用，避免死循环或空块。
const (
	defaultChunkSize    = 512
	defaultChunkOverlap = 64
)

// Chunk 表示一个文本块及其在原文中的序号。
type Chunk struct {
	// Index 块序号，从 0 开始，用于生成唯一向量 ID 与排序。
	Index int
	// Content 块文本内容。
	Content string
}

// SplitIntoChunks 按字符（rune）数将文本切分为带重叠的块。
//
// chunkSize 为单块最大字符数，overlap 为相邻块的重叠字符数（用于维持跨块语义连续性）。
// 入参非法时回退到默认值；overlap 不小于 chunkSize 时强制收敛，避免步长非正导致死循环。
// 仅由空白字符组成的块会被丢弃。返回的块序号连续递增。
func SplitIntoChunks(text string, chunkSize, overlap int) []Chunk {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	if overlap < 0 {
		overlap = defaultChunkOverlap
	}
	// 步长必须为正，否则窗口无法前移。
	if overlap >= chunkSize {
		overlap = chunkSize / 2
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return nil
	}

	var chunks []Chunk
	idx := 0
	for start := 0; start < n; start += step {
		end := start + chunkSize
		if end > n {
			end = n
		}
		content := strings.TrimSpace(string(runes[start:end]))
		if content != "" {
			chunks = append(chunks, Chunk{Index: idx, Content: content})
			idx++
		}
		if end == n {
			break
		}
	}
	return chunks
}

// loadOptions 控制文档加载与切块行为，承载「语义切分」「块头标签」等可插拔升级开关。
//
// 这些能力均默认关闭、互不强耦合；为 nil 的 Embedder 或关闭的开关会自动回退到现有行为，
// 保证存量调用方与旧索引优雅降级。
type loadOptions struct {
	// ChunkSize 单块最大字符（rune）数。
	ChunkSize int
	// Overlap 相邻块重叠字符数（递归/定长切分使用）。
	Overlap int
	// Embedder 语义切分所需的句向量生成器；为 nil 时不做语义切分。
	Embedder embedding.Embedder
	// EnableSemanticChunking 是否启用语义切分（仅对非 Markdown 文件生效）。
	EnableSemanticChunking bool
	// SemanticPercentile 语义断点距离分位数阈值（0-100）。
	SemanticPercentile float64
	// SemanticBufferSize 句向量滑窗每侧大小。
	SemanticBufferSize int
	// EnableHeaderInjection 是否在块正文首部注入「来源｜章节」块头标签。
	EnableHeaderInjection bool
}

// LoadDocuments 解析磁盘文件、用 Eino 切分器切块并转换为可索引的文档集合（兼容旧签名）。
//
// 该入口保持「递归/标题感知切分 + 定长滑窗兜底」的现有行为，不启用语义切分与块头标签，
// 供测试及不依赖 Engine 升级配置的调用方使用；Engine.Index 走 loadDocuments(opts) 透传开关。
//
// 每个 chunk 生成独立 ID（chunk_N）与元数据（source 原始文件名、chunk 序号），
// 以支持向量级检索与引用溯源（向量 key 依赖 chunk_N，重编号后保持兼容）。
// 空内容文件会返回错误，避免建立空索引。
func LoadDocuments(ctx context.Context, filePath string, chunkSize, overlap int) ([]*schema.Document, error) {
	return loadDocuments(ctx, filePath, loadOptions{ChunkSize: chunkSize, Overlap: overlap})
}

// loadDocuments 是带可插拔升级能力（语义切分 / 块头标签）的文档加载实现。
//
// 切块路由：
//   - Markdown 始终走标题感知切分以保留章节结构（块头标签依赖 h1/h2/h3 元数据）；
//   - 非 Markdown 且开启语义切分且注入了 Embedder → 句向量语义切分，失败回退递归切分；
//   - 其余 → 递归切分。
//
// 任一策略产出为空时，最终回退定长滑窗兜底，保证索引流程不中断（优雅降级）。
func loadDocuments(ctx context.Context, filePath string, opts loadOptions) ([]*schema.Document, error) {
	raw, err := ParseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("parse file failed: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		logger.Warn("loadDocuments empty content", "path", filePath)
		return nil, fmt.Errorf("no content parsed from file: %s", filePath)
	}

	source := filepath.Base(filePath)
	ext := strings.ToLower(filepath.Ext(filePath))
	isMarkdown := ext == ".md" || ext == ".markdown"
	semanticOn := opts.EnableSemanticChunking && opts.Embedder != nil && !isMarkdown
	strategy := "recursive"
	if isMarkdown {
		strategy = "markdown"
	} else if semanticOn {
		strategy = "semantic"
	}
	logger.Info("loadDocuments strategy selected",
		"path", filePath,
		"source", source,
		"ext", ext,
		"strategy", strategy,
		"enableSemanticChunking", opts.EnableSemanticChunking,
		"semanticActive", semanticOn,
		"enableHeaderInjection", opts.EnableHeaderInjection)

	var docs []*schema.Document
	switch {
	case isMarkdown:
		docs = splitByEinoSplitter(ctx, ext, raw, source, opts.ChunkSize, opts.Overlap)
	case semanticOn:
		// 语义切分失败（embedding 报错 / 句子过少 / 空结果）→ 回退递归切分。
		docs = splitBySemantic(ctx, raw, source, opts)
		if len(docs) == 0 {
			logger.Warn("semantic chunking empty, fallback to recursive splitter", "path", filePath)
			docs = splitByEinoSplitter(ctx, ext, raw, source, opts.ChunkSize, opts.Overlap)
		}
	default:
		docs = splitByEinoSplitter(ctx, ext, raw, source, opts.ChunkSize, opts.Overlap)
	}

	// 递归/语义切分均不可用时，最终回退定长滑窗兜底。
	if len(docs) == 0 {
		logger.Warn("splitter produced no chunks, fallback to fixed window", "path", filePath)
		fixed, ferr := loadByFixedWindow(raw, source, opts.ChunkSize, opts.Overlap)
		if ferr != nil {
			return nil, ferr
		}
		docs = fixed
	}

	// 块头标签注入（可选）：把「来源｜章节」前缀拼到正文首部，并写入 headers 元数据，
	// 使标题路径同时进入向量与提示词，提升语义可分辨度与引用可读性。
	if opts.EnableHeaderInjection {
		withHeaders := injectHeaders(docs, source)
		logger.Info("header injection done",
			"source", source,
			"chunks", len(docs),
			"chunksWithHeaders", withHeaders)
	}
	logger.Info("loadDocuments done", "path", filePath, "ext", ext, "chunks", len(docs),
		"semantic", semanticOn, "headerInjection", opts.EnableHeaderInjection)
	return docs, nil
}

// splitByEinoSplitter 用 Eino 切分器（md→标题感知 / 其他→递归）切块并完成 chunk_N 编号。
// 切分器创建或执行失败、或仅产出空白块时返回 nil，由调用方回退到下一级兜底。
func splitByEinoSplitter(ctx context.Context, ext, raw, source string, chunkSize, overlap int) []*schema.Document {
	splitter, err := newSplitter(ctx, ext, chunkSize, overlap)
	if err != nil {
		logger.Warn("create splitter failed", "ext", ext, "err", err)
		return nil
	}
	// 将抽取出的纯文本包成单个父文档交给切分器；切分结果继承父文档 MetaData。
	parent := &schema.Document{
		ID:       source,
		Content:  raw,
		MetaData: map[string]any{"source": source},
	}
	chunks, err := splitter.Transform(ctx, []*schema.Document{parent})
	if err != nil || len(chunks) == 0 {
		logger.Warn("splitter transform failed", "ext", ext, "err", err, "chunks", len(chunks))
		return nil
	}
	return finalizeChunks(chunks, source)
}

// splitBySemantic 用句向量相似度语义切分并完成 chunk_N 编号；失败时返回 nil 由调用方回退。
func splitBySemantic(ctx context.Context, raw, source string, opts loadOptions) []*schema.Document {
	logger.Info("semantic chunking selected",
		"source", source,
		"chunkSize", opts.ChunkSize,
		"percentile", opts.SemanticPercentile,
		"bufferSize", opts.SemanticBufferSize)
	chunks, err := splitTextBySemantic(ctx, opts.Embedder, raw, semanticOptions{
		ChunkSize:  opts.ChunkSize,
		Percentile: opts.SemanticPercentile,
		BufferSize: opts.SemanticBufferSize,
	})
	if err != nil {
		logger.Warn("semantic chunking failed", "source", source, "err", err)
		return nil
	}
	docs := make([]*schema.Document, 0, len(chunks))
	for _, c := range chunks {
		docs = append(docs, &schema.Document{Content: c.Content, MetaData: map[string]any{}})
	}
	return finalizeChunks(docs, source)
}

// finalizeChunks 统一重新编号 ID 为 chunk_N，确保 source / chunk 元数据存在，并跳过纯空白块。
//
// 各切块策略（递归 / 语义 / 标题）统一经此收尾，保证产出文档在 ID / MetaData 约定上完全一致，
// 向量 key 依赖 chunk_N，上下文增强依赖 chunk 序号。
func finalizeChunks(chunks []*schema.Document, source string) []*schema.Document {
	docs := make([]*schema.Document, 0, len(chunks))
	idx := 0
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		if c.MetaData == nil {
			c.MetaData = map[string]any{}
		}
		c.MetaData["source"] = source
		c.MetaData["chunk"] = idx
		c.ID = fmt.Sprintf("chunk_%d", idx)
		docs = append(docs, c)
		idx++
	}
	return docs
}

// headerPath 从切分器写入的 h1/h2/h3 元数据拼出标题路径「H1 > H2 > H3」。
// 缺失或纯空白的层级被跳过；无任何标题时返回空串（非 Markdown 文件通常如此）。
func headerPath(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, k := range []string{"h1", "h2", "h3"} {
		if v, ok := meta[k].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, " > ")
}

// injectHeaders 为每个块注入块头标签：把「来源｜章节」简洁前缀拼到正文首部，并写入 headers 元数据。
//
//   - 有标题路径（Markdown）→ 前缀「来源：foo.md｜章节：H1 > H2」，headers 为标题路径；
//   - 无标题（纯文本）→ 前缀「来源：foo.md」，headers 为空串。
//
// 前缀进入正文意味着它同时参与向量化与提示词拼装，增强块的语义可分辨度与引用可读性。
func injectHeaders(docs []*schema.Document, source string) int {
	withHeaders := 0
	for _, d := range docs {
		if d.MetaData == nil {
			d.MetaData = map[string]any{}
		}
		path := headerPath(d.MetaData)
		d.MetaData["headers"] = path
		var prefix string
		if path != "" {
			withHeaders++
			prefix = fmt.Sprintf("「来源：%s｜章节：%s」\n", source, path)
		} else {
			prefix = fmt.Sprintf("「来源：%s」\n", source)
		}
		d.Content = prefix + d.Content
	}
	return withHeaders
}

// loadByFixedWindow 旧定长滑窗实现，作为 Eino 切分器不可用时的兜底。
//
// 复用 SplitIntoChunks 按 rune 定长切块，再构造与切分器路径一致的 chunk_N 文档结构，
// 保证两条路径产出的文档在 ID / MetaData 约定上完全兼容。
func loadByFixedWindow(raw, source string, chunkSize, overlap int) ([]*schema.Document, error) {
	chunks := SplitIntoChunks(raw, chunkSize, overlap)
	if len(chunks) == 0 {
		logger.Warn("loadByFixedWindow produced no chunks", "source", source)
		return nil, fmt.Errorf("no content parsed from file: %s", source)
	}
	docs := make([]*schema.Document, 0, len(chunks))
	for _, c := range chunks {
		docs = append(docs, &schema.Document{
			ID:      fmt.Sprintf("chunk_%d", c.Index),
			Content: c.Content,
			MetaData: map[string]any{
				"source": source,
				"chunk":  c.Index,
			},
		})
	}
	logger.Info("loadByFixedWindow chunked", "source", source, "chunks", len(docs))
	return docs, nil
}

// BuildPrompt 将检索到的文档块拼装为带上下文的提示词。
// 无检索结果时直接返回原始查询。带上来源信息，便于模型在回答中标注引用。
func BuildPrompt(query string, docs []*schema.Document) string {
	if len(docs) == 0 {
		return query
	}
	var contextText strings.Builder
	for i, doc := range docs {
		source := ""
		if s, ok := doc.MetaData["source"].(string); ok {
			source = s
		}
		// 引用行扩展：携带块头标签时追加「章节：...」，便于模型与用户定位来源章节。
		label := fmt.Sprintf("[文档 %d｜来源：%s", i+1, source)
		if h, ok := doc.MetaData["headers"].(string); ok && strings.TrimSpace(h) != "" {
			label += "｜章节：" + h
		}
		label += "]"
		contextText.WriteString(fmt.Sprintf("%s: %s\n\n", label, doc.Content))
	}
	return fmt.Sprintf(`基于以下参考文档回答用户的问题。如果文档中没有相关信息，请说明无法找到相关信息。

参考文档：
%s
用户问题：%s

请提供准确、完整的回答，并在合适处标注引用的来源：`, contextText.String(), query)
}
