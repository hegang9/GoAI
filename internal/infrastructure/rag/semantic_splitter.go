package rag

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"GopherAI/pkg/logger"

	"github.com/cloudwego/eino/components/embedding"
)

// 语义切分相关兜底默认值与限制。
const (
	// defaultSemanticPercentile 断点距离分位数阈值默认值（0-100，越大切块越少）。
	defaultSemanticPercentile = 95.0
	// defaultSemanticBufferSize 句向量滑窗每侧窗口大小默认值。
	defaultSemanticBufferSize = 1
	// semanticEmbedBatch 句向量分批请求大小，规避单次 embedding 请求过大被服务端拒绝。
	semanticEmbedBatch = 10
	// semanticMinSentences 句子数过少时不值得做语义切分，直接交由上层回退处理。
	semanticMinSentences = 3
)

// semanticOptions 语义切分参数集合。
type semanticOptions struct {
	// ChunkSize 单块最大字符（rune）数，超出时对该块二次硬切，避免超出 embedding 输入上限。
	ChunkSize int
	// Percentile 相邻句距离的断点分位数阈值（0-100）。
	Percentile float64
	// BufferSize 句向量滑窗每侧窗口大小（>0 时用相邻句拼接稳定单句语义）。
	BufferSize int
}

// splitTextBySemantic 用句向量相似度把文本切分为语义连续的块。
//
// 流程：切句 → 滑窗合并稳定句义 → 批量向量化 → 计算相邻句余弦距离 →
// 取分位数阈值定位语义边界（距离突增处断块）→ 合并成块并对超长块二次硬切。
// 句子过少、向量化失败或产出为空时返回 error，由调用方回退到递归/定长切分，保证索引不中断。
func splitTextBySemantic(ctx context.Context, embedder embedding.Embedder, text string, opts semanticOptions) ([]Chunk, error) {
	start := time.Now()

	sentences := splitSentences(text)
	if len(sentences) < semanticMinSentences {
		// 句子过少，语义切分意义不大，交回退路径处理（避免无谓的 embedding 调用）。
		return nil, fmt.Errorf("too few sentences for semantic chunking: %d", len(sentences))
	}

	// 用滑窗把相邻句拼接后再向量化，缓解单句过短导致的句向量不稳定。
	windows := buildSentenceWindows(sentences, opts.BufferSize)
	logger.Info("semantic chunking start",
		"sentences", len(sentences),
		"chunkSize", opts.ChunkSize,
		"percentile", opts.Percentile,
		"bufferSize", opts.BufferSize,
		"batchSize", semanticEmbedBatch)
	embeddings, err := embedInBatches(ctx, embedder, windows, semanticEmbedBatch)
	if err != nil {
		logger.Warn("semantic chunking embed failed", "sentences", len(sentences), "err", err)
		return nil, fmt.Errorf("embed sentences failed: %w", err)
	}
	if len(embeddings) != len(sentences) {
		return nil, fmt.Errorf("embedding count mismatch: got %d want %d", len(embeddings), len(sentences))
	}

	// 相邻句向量的余弦距离：距离越大语义跳跃越明显，越可能是语义边界。
	dists := make([]float64, 0, len(sentences)-1)
	for i := 0; i+1 < len(embeddings); i++ {
		dists = append(dists, cosineDistance(embeddings[i], embeddings[i+1]))
	}
	threshold := percentile(dists, opts.Percentile)

	// 按断点把句子分组成块；对超过 ChunkSize 的块二次硬切，避免单块过大。
	var chunks []Chunk
	var buf strings.Builder
	idx := 0
	hardSplitBlocks := 0
	flush := func() {
		content := strings.TrimSpace(buf.String())
		buf.Reset()
		if content == "" {
			return
		}
		if opts.ChunkSize > 0 && runeLen(content) > opts.ChunkSize {
			hardSplitBlocks++
			for _, sub := range SplitIntoChunks(content, opts.ChunkSize, 0) {
				chunks = append(chunks, Chunk{Index: idx, Content: sub.Content})
				idx++
			}
			return
		}
		chunks = append(chunks, Chunk{Index: idx, Content: content})
		idx++
	}
	for i, s := range sentences {
		buf.WriteString(s)
		// 第 i 句与第 i+1 句之间距离超阈值，则在此断块。
		if i < len(dists) && dists[i] > threshold {
			flush()
		}
	}
	// 收尾最后一块
	flush()

	if len(chunks) == 0 {
		return nil, fmt.Errorf("semantic chunking produced no chunks")
	}
	logger.Info("semantic chunking done",
		"sentences", len(sentences),
		"breakpoints", countAbove(dists, threshold),
		"chunks", len(chunks),
		"threshold", threshold,
		"hardSplitBlocks", hardSplitBlocks,
		"cost", time.Since(start))
	return chunks, nil
}

// splitSentences 把文本按句末标点/换行切成句子，分隔符保留在句末（与递归切分的 KeepTypeEnd 一致）。
// 纯空白句被丢弃；无句末标点的整段会作为单句返回（由上层据句子数决定是否回退）。
func splitSentences(text string) []string {
	var sentences []string
	var b strings.Builder
	for _, r := range text {
		b.WriteRune(r)
		if isSentenceBoundary(r) {
			if s := strings.TrimSpace(b.String()); s != "" {
				sentences = append(sentences, s)
			}
			b.Reset()
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		sentences = append(sentences, s)
	}
	return sentences
}

// isSentenceBoundary 判断某字符是否为句末边界（中英文句末标点与换行）。
func isSentenceBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?', '.', '\n':
		return true
	default:
		return false
	}
}

// buildSentenceWindows 用每侧 buffer 个相邻句拼接成窗口文本，稳定单句向量；buffer<=0 时原样返回。
// 返回切片长度与 sentences 一致，第 i 项对应第 i 句的向量化输入。
func buildSentenceWindows(sentences []string, buffer int) []string {
	if buffer <= 0 {
		return sentences
	}
	n := len(sentences)
	out := make([]string, n)
	for i := range sentences {
		lo := i - buffer
		if lo < 0 {
			lo = 0
		}
		hi := i + buffer
		if hi >= n {
			hi = n - 1
		}
		out[i] = strings.Join(sentences[lo:hi+1], "")
	}
	return out
}

// embedInBatches 分批调用 EmbedStrings，规避单次请求文本过多；任一批失败立即返回错误。
func embedInBatches(ctx context.Context, embedder embedding.Embedder, texts []string, batch int) ([][]float64, error) {
	if batch <= 0 {
		batch = len(texts)
	}
	out := make([][]float64, 0, len(texts))
	for i := 0; i < len(texts); i += batch {
		end := i + batch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := embedder.EmbedStrings(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// cosineDistance 返回 1 - 余弦相似度；维度不一致或任一向量零模时返回 1（视为最不相关，倾向断开）。
func cosineDistance(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 1
	}
	sim := dot / (math.Sqrt(na) * math.Sqrt(nb))
	return 1 - sim
}

// percentile 返回距离切片的第 p 百分位值（线性插值）。空切片返回 0。
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo] + (sorted[hi]-sorted[lo])*frac
}

// countAbove 统计严格大于阈值的元素个数，用于日志观测断点数量。
func countAbove(values []float64, threshold float64) int {
	c := 0
	for _, v := range values {
		if v > threshold {
			c++
		}
	}
	return c
}
