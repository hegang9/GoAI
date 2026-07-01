package rag

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/embedding"
)

// fakeEmbedder 是测试用的确定性向量生成器：按文本返回预设向量，未命中用默认向量。
// 用于在不依赖外部 embedding 服务的前提下校验语义切分的断点逻辑。
type fakeEmbedder struct {
	vecs map[string][]float64
	def  []float64
}

// EmbedStrings 实现 embedding.Embedder：逐条返回预设向量。
func (f *fakeEmbedder) EmbedStrings(_ context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		if v, ok := f.vecs[t]; ok {
			out[i] = v
		} else {
			out[i] = f.def
		}
	}
	return out, nil
}

// TestSplitSentences 校验按中英文句末标点/换行切句，分隔符保留在句末、空白被丢弃。
func TestSplitSentences(t *testing.T) {
	t.Parallel()

	got := splitSentences("第一句。第二句！第三句？\n  \n第四句。")
	want := []string{"第一句。", "第二句！", "第三句？", "第四句。"}
	if len(got) != len(want) {
		t.Fatalf("splitSentences len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sentence[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestCosineDistance 校验余弦距离：相同方向≈0、正交=1、零模兜底=1。
func TestCosineDistance(t *testing.T) {
	t.Parallel()

	if d := cosineDistance([]float64{1, 0}, []float64{1, 0}); math.Abs(d) > 1e-9 {
		t.Fatalf("same vector distance = %v, want ~0", d)
	}
	if d := cosineDistance([]float64{1, 0}, []float64{0, 1}); math.Abs(d-1) > 1e-9 {
		t.Fatalf("orthogonal distance = %v, want 1", d)
	}
	if d := cosineDistance([]float64{0, 0}, []float64{1, 1}); d != 1 {
		t.Fatalf("zero-norm distance = %v, want 1", d)
	}
}

// TestPercentile 校验分位数（含线性插值与边界）。
func TestPercentile(t *testing.T) {
	t.Parallel()

	vals := []float64{0, 0, 1}
	if p := percentile(vals, 50); p != 0 {
		t.Fatalf("p50 = %v, want 0", p)
	}
	if p := percentile(vals, 100); p != 1 {
		t.Fatalf("p100 = %v, want 1", p)
	}
	if p := percentile(nil, 95); p != 0 {
		t.Fatalf("percentile(nil) = %v, want 0", p)
	}
}

// TestSplitTextBySemantic_BreaksAtTopicShift 校验在语义跳变处断块：
// 两句“猫”+两句“金融”，应在主题切换处断成 2 块。
func TestSplitTextBySemantic_BreaksAtTopicShift(t *testing.T) {
	t.Parallel()

	emb := &fakeEmbedder{
		vecs: map[string][]float64{
			"猫喜欢吃鱼。":  {1, 0},
			"猫喜欢睡觉。":  {1, 0},
			"股票今天大涨。": {0, 1},
			"债券收益下跌。": {0, 1},
		},
		def: []float64{1, 1},
	}
	text := "猫喜欢吃鱼。猫喜欢睡觉。股票今天大涨。债券收益下跌。"

	// buffer=0 使窗口即句子本身，便于用预设向量精确控制距离；p50 阈值=中位数 0。
	chunks, err := splitTextBySemantic(context.Background(), emb, text, semanticOptions{ChunkSize: 512, Percentile: 50, BufferSize: 0})
	if err != nil {
		t.Fatalf("splitTextBySemantic error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2 (%v)", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "猫") || strings.Contains(chunks[0].Content, "股票") {
		t.Fatalf("chunk[0] = %q, want only 猫 topic", chunks[0].Content)
	}
	if !strings.Contains(chunks[1].Content, "股票") || strings.Contains(chunks[1].Content, "猫") {
		t.Fatalf("chunk[1] = %q, want only 金融 topic", chunks[1].Content)
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Fatalf("chunk[%d].Index = %d, want %d", i, c.Index, i)
		}
	}
}

// TestSplitTextBySemantic_LongChunkHardSplit 校验无语义边界但单块超长时的二次硬切。
func TestSplitTextBySemantic_LongChunkHardSplit(t *testing.T) {
	t.Parallel()

	// 所有句子同向量 → 无断点 → 合并成一个超长块 → 按 ChunkSize 硬切。
	emb := &fakeEmbedder{def: []float64{1, 0}}
	text := "aaaa。bbbb。cccc。dddd。" // 20 runes

	chunks, err := splitTextBySemantic(context.Background(), emb, text, semanticOptions{ChunkSize: 8, Percentile: 95, BufferSize: 0})
	if err != nil {
		t.Fatalf("splitTextBySemantic error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected hard-split into multiple chunks, got %d (%v)", len(chunks), chunks)
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Fatalf("chunk[%d].Index = %d, want %d", i, c.Index, i)
		}
		if runeLen(c.Content) > 8 {
			t.Fatalf("chunk[%d] len = %d, want <= 8 (%q)", i, runeLen(c.Content), c.Content)
		}
	}
}

// TestSplitTextBySemantic_TooFewSentences 校验句子过少时返回错误，交由上层回退。
func TestSplitTextBySemantic_TooFewSentences(t *testing.T) {
	t.Parallel()

	emb := &fakeEmbedder{def: []float64{1, 0}}
	if _, err := splitTextBySemantic(context.Background(), emb, "只有一句话没有结尾标点", semanticOptions{ChunkSize: 512, Percentile: 95}); err == nil {
		t.Fatal("expected error for too few sentences, got nil")
	}
}
