package test

import (
	"strings"
	"testing"

	rag "GopherAI/internal/infrastructure/rag"

	"github.com/cloudwego/eino/schema"
)

// TestSplitIntoChunks_BasicOverlap 校验按字符切块与重叠步长的基本行为。
func TestSplitIntoChunks_BasicOverlap(t *testing.T) {
	t.Parallel()

	// 10 个字符，chunkSize=4，overlap=1 → 步长 3：[0:4] [3:7] [6:10]，到末尾即停止
	text := "0123456789"
	chunks := rag.SplitIntoChunks(text, 4, 1)

	want := []string{"0123", "3456", "6789"}
	if len(chunks) != len(want) {
		t.Fatalf("SplitIntoChunks() len = %d, want %d (%v)", len(chunks), len(want), chunks)
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Fatalf("chunk[%d].Index = %d, want %d", i, c.Index, i)
		}
		if c.Content != want[i] {
			t.Fatalf("chunk[%d].Content = %q, want %q", i, c.Content, want[i])
		}
	}
}

// TestSplitIntoChunks_UnicodeSafe 校验以 rune 为单位切块，不会把多字节字符切坏。
func TestSplitIntoChunks_UnicodeSafe(t *testing.T) {
	t.Parallel()

	text := "你好世界你好世界" // 8 个汉字
	chunks := rag.SplitIntoChunks(text, 3, 0)
	want := []string{"你好世", "界你好", "世界"}
	if len(chunks) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(chunks), len(want), chunks)
	}
	for i, c := range chunks {
		if c.Content != want[i] {
			t.Fatalf("chunk[%d] = %q, want %q", i, c.Content, want[i])
		}
	}
}

// TestSplitIntoChunks_EmptyAndWhitespace 校验空文本返回空、纯空白块被丢弃。
func TestSplitIntoChunks_EmptyAndWhitespace(t *testing.T) {
	t.Parallel()

	if got := rag.SplitIntoChunks("", 10, 2); got != nil {
		t.Fatalf("empty text chunks = %v, want nil", got)
	}
	if got := rag.SplitIntoChunks("     ", 2, 0); len(got) != 0 {
		t.Fatalf("whitespace chunks = %v, want empty", got)
	}
}

// TestSplitIntoChunks_InvalidParamsNoInfiniteLoop 校验非法参数被收敛，不会死循环。
func TestSplitIntoChunks_InvalidParamsNoInfiniteLoop(t *testing.T) {
	t.Parallel()

	// overlap >= chunkSize 时步长应被强制收敛为正值。
	chunks := rag.SplitIntoChunks("abcdefgh", 3, 5)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks, got none")
	}
	// chunkSize<=0 回退默认值，应只产生一个块（默认 512 > 文本长度）。
	one := rag.SplitIntoChunks("short text", 0, 0)
	if len(one) != 1 || one[0].Content != "short text" {
		t.Fatalf("default chunkSize result = %v, want single chunk", one)
	}
}

// TestBuildPrompt_NoDocsReturnsQuery 校验无文档时返回原始查询。
func TestBuildPrompt_NoDocsReturnsQuery(t *testing.T) {
	t.Parallel()

	q := "今天天气如何"
	if got := rag.BuildPrompt(q, nil); got != q {
		t.Fatalf("BuildPrompt(no docs) = %q, want %q", got, q)
	}
}

// TestBuildPrompt_WithDocsEmbedsContextAndSource 校验提示词包含查询、内容与来源。
func TestBuildPrompt_WithDocsEmbedsContextAndSource(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{
		{Content: "Go 是一门编译型语言", MetaData: map[string]any{"source": "go.md"}},
		{Content: "Go 由 Google 开发", MetaData: map[string]any{"source": "go.md"}},
	}
	prompt := rag.BuildPrompt("介绍一下 Go", docs)
	for _, must := range []string{"介绍一下 Go", "编译型语言", "Google", "go.md", "参考文档"} {
		if !strings.Contains(prompt, must) {
			t.Fatalf("prompt missing %q\nprompt=%s", must, prompt)
		}
	}
}
