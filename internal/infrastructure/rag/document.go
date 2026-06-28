// Package rag 是检索增强适配层：基于 eino + Redis 向量库实现 domain/rag.Indexer 端口，
// 并向 infrastructure/ai 提供检索与提示词构造能力。
package rag

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"GopherAI/pkg/logger"

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

// LoadDocuments 解析磁盘文件、用 Eino 切分器切块并转换为可索引的文档集合。
//
// 切块策略：先用 ParseFile 抽取纯文本，再交给按扩展名选择的 Eino 切分器（document.Transformer）
// 递归/标题感知切分，尽量对齐句子、段落、Markdown 标题等自然边界，保持语义完整。
// 切分器创建或执行失败时自动回退到旧定长滑窗实现（loadByFixedWindow），保证索引流程不中断。
//
// 每个 chunk 生成独立 ID（chunk_N）与元数据（source 原始文件名、chunk 序号），
// 以支持向量级检索与引用溯源（向量 key 依赖 chunk_N，重编号后保持兼容）。
// 空内容文件会返回错误，避免建立空索引。
func LoadDocuments(ctx context.Context, filePath string, chunkSize, overlap int) ([]*schema.Document, error) {
	raw, err := ParseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("parse file failed: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		logger.Warn("loadDocuments empty content", "path", filePath)
		return nil, fmt.Errorf("no content parsed from file: %s", filePath)
	}

	source := filepath.Base(filePath)
	ext := filepath.Ext(filePath)

	// 兜底：切分器创建失败时回退到原定长滑窗实现。
	splitter, err := newSplitter(ctx, ext, chunkSize, overlap)
	if err != nil {
		logger.Warn("create splitter failed, fallback to fixed window", "path", filePath, "err", err)
		return loadByFixedWindow(raw, source, chunkSize, overlap)
	}

	// 将抽取出的纯文本包成单个父文档交给切分器；切分结果继承父文档 MetaData。
	parent := &schema.Document{
		ID:       source,
		Content:  raw,
		MetaData: map[string]any{"source": source},
	}
	chunks, err := splitter.Transform(ctx, []*schema.Document{parent})
	if err != nil || len(chunks) == 0 {
		logger.Warn("splitter transform failed, fallback to fixed window", "path", filePath, "err", err, "chunks", len(chunks))
		return loadByFixedWindow(raw, source, chunkSize, overlap)
	}

	// 统一重新编号 ID 为 chunk_N，并确保 source 元数据存在（向量 key 依赖 chunk_N）。
	// 切分器可能产出纯空白块（如标题切分后的空段），这里跳过以免污染索引。
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
	if len(docs) == 0 {
		logger.Warn("splitter produced only blank chunks, fallback to fixed window", "path", filePath)
		return loadByFixedWindow(raw, source, chunkSize, overlap)
	}
	logger.Info("loadDocuments chunked by eino splitter", "path", filePath, "ext", ext, "chunks", len(docs))
	return docs, nil
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
		contextText.WriteString(fmt.Sprintf("[文档 %d｜来源：%s]: %s\n\n", i+1, source, doc.Content))
	}
	return fmt.Sprintf(`基于以下参考文档回答用户的问题。如果文档中没有相关信息，请说明无法找到相关信息。

参考文档：
%s
用户问题：%s

请提供准确、完整的回答，并在合适处标注引用的来源：`, contextText.String(), query)
}
