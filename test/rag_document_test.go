package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// TestLoadDocuments_RecursiveSplitNoMidSentenceCut 校验递归切分器按句末标点切分，
// 不从句子中间硬断：每个块都以“。”收尾（KeepTypeEnd 保留分隔符），且 chunk_N / source 正确。
func TestLoadDocuments_RecursiveSplitNoMidSentenceCut(t *testing.T) {
	t.Parallel()

	// 8 个长度不一的中文句子，单句均短于 chunkSize，确保切分点落在“。”而非句中。
	sentences := []string{
		"苹果是一种常见的水果",
		"香蕉富含丰富的钾元素",
		"橙子含有大量维生素C",
		"葡萄可以用来酿造红酒",
		"西瓜在夏天非常受欢迎",
		"草莓的外形小巧而鲜红",
		"芒果带有浓郁的热带风味",
		"樱桃常被用作蛋糕装饰",
	}
	text := strings.Join(sentences, "。") + "。"

	dir := t.TempDir()
	path := filepath.Join(dir, "fruit.txt")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	docs, err := rag.LoadDocuments(context.Background(), path, 20, 0)
	if err != nil {
		t.Fatalf("LoadDocuments() error = %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(docs))
	}
	for i, d := range docs {
		content := strings.TrimSpace(d.Content)
		if !strings.HasSuffix(content, "。") {
			t.Fatalf("chunk[%d]=%q not ending at sentence boundary", i, content)
		}
		if want := fmt.Sprintf("chunk_%d", i); d.ID != want {
			t.Fatalf("chunk[%d] ID = %q, want %q", i, d.ID, want)
		}
		if src, _ := d.MetaData["source"].(string); src != "fruit.txt" {
			t.Fatalf("chunk[%d] source = %v, want fruit.txt", i, d.MetaData["source"])
		}
	}
	// 拼回所有块后应覆盖每个原始句子，确认没有句子被从中间切坏。
	joined := ""
	for _, d := range docs {
		joined += d.Content
	}
	for _, s := range sentences {
		if !strings.Contains(joined, s) {
			t.Fatalf("sentence %q was split across chunks (not found intact)", s)
		}
	}
}

// TestLoadDocuments_MarkdownHeaderSplit 校验 .md 走标题感知切分：
// 按 #/##/### 切块、标题层级写入 MetaData、代码块完整保留、chunk_N / source 正确。
func TestLoadDocuments_MarkdownHeaderSplit(t *testing.T) {
	t.Parallel()

	md := strings.Join([]string{
		"# 一级标题",
		"这是第一段正文内容。",
		"",
		"## 二级标题",
		"这是第二段，稍微长一点的内容。",
		"",
		"### 三级标题",
		"```go",
		`fmt.Println("hello world")`,
		"```",
		"结尾段落收尾。",
	}, "\n")

	dir := t.TempDir()
	path := filepath.Join(dir, "guide.md")
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	docs, err := rag.LoadDocuments(context.Background(), path, 512, 64)
	if err != nil {
		t.Fatalf("LoadDocuments() error = %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("markdown split expected multiple sections, got %d", len(docs))
	}
	for i, d := range docs {
		if want := fmt.Sprintf("chunk_%d", i); d.ID != want {
			t.Fatalf("chunk[%d] ID = %q, want %q", i, d.ID, want)
		}
		if src, _ := d.MetaData["source"].(string); src != "guide.md" {
			t.Fatalf("chunk[%d] source = %v, want guide.md", i, d.MetaData["source"])
		}
	}
	// 第一块应携带一级标题元数据。
	if h1, _ := docs[0].MetaData["h1"].(string); h1 != "一级标题" {
		t.Fatalf("first chunk h1 meta = %v, want 一级标题", docs[0].MetaData["h1"])
	}
	// 代码块内容必须完整保留在某个块中（未被标题切分破坏）。
	foundCode := false
	for _, d := range docs {
		if strings.Contains(d.Content, `fmt.Println("hello world")`) {
			foundCode = true
			if h3, _ := d.MetaData["h3"].(string); h3 != "三级标题" {
				t.Fatalf("code chunk h3 meta = %v, want 三级标题", d.MetaData["h3"])
			}
		}
	}
	if !foundCode {
		t.Fatal("code block content missing from all chunks")
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
