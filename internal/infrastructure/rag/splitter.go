package rag

import (
	"context"
	"strings"

	"GopherAI/pkg/logger"

	markdown "github.com/cloudwego/eino-ext/components/document/transformer/splitter/markdown"
	recursive "github.com/cloudwego/eino-ext/components/document/transformer/splitter/recursive"
	"github.com/cloudwego/eino/components/document"
)

// runeLen 以 rune（字符）数计算长度，作为切分器的长度度量函数。
//
// recursive 切分器默认用 len()（字节数）计算长度，对中文等多字节文本会偏大约 3 倍，
// 导致 ChunkSize 语义与配置注释“单块最大字符数”不一致。这里改用 rune 计数，
// 使 chunkSize / overlap 在中英文混排时都按“字符数”衡量，与旧定长滑窗保持一致。
func runeLen(s string) int {
	return len([]rune(s))
}

// normalizeSplitParams 收敛非法的分块参数，避免切分器构造失败或步长非正。
// 行为与 SplitIntoChunks 内部的兜底保持一致，保证两条切块路径语义统一。
func normalizeSplitParams(chunkSize, overlap int) (int, int) {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	if overlap < 0 {
		overlap = defaultChunkOverlap
	}
	// overlap 不应大于等于 chunkSize，否则递归切分无法收敛块大小。
	if overlap >= chunkSize {
		overlap = chunkSize / 2
	}
	return chunkSize, overlap
}

// newSplitter 按文件扩展名构造合适的 Eino 切分器（document.Transformer）。
//   - .md  → Markdown 标题感知切分，按 #/##/### 标题切块并把标题层级写入 MetaData；
//   - 其他 → 递归切分，按段落/换行/中英文标点的分隔符层级递归，尽量对齐自然边界。
//
// 返回的切分器供 LoadDocuments 调用；构造失败时由调用方回退到定长滑窗兜底。
func newSplitter(ctx context.Context, ext string, chunkSize, overlap int) (document.Transformer, error) {
	chunkSize, overlap = normalizeSplitParams(chunkSize, overlap)

	switch strings.ToLower(ext) {
	case ".md", ".markdown":
		// Markdown 标题感知切分：保留标题层级信息，便于按章节召回。
		logger.Info("newSplitter use markdown header splitter", "ext", ext)
		return markdown.NewHeaderSplitter(ctx, &markdown.HeaderConfig{
			Headers: map[string]string{
				"#":   "h1",
				"##":  "h2",
				"###": "h3",
			},
			TrimHeaders: false,
		})
	default:
		// 递归切分：按分隔符层级递归，优先在段落/换行/句末标点处断开。
		logger.Info("newSplitter use recursive splitter", "ext", ext, "chunkSize", chunkSize, "overlap", overlap)
		return recursive.NewSplitter(ctx, &recursive.Config{
			ChunkSize:   chunkSize,
			OverlapSize: overlap,
			Separators:  []string{"\n\n", "\n", "。", "！", "？", ".", "!", "?", " "},
			LenFunc:     runeLen,
			// KeepTypeEnd 把分隔符保留在块末尾，避免丢失句末标点 / 换行，保证内容可读、不失真。
			KeepType: recursive.KeepTypeEnd,
		})
	}
}
