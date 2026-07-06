package rag

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// ---------------------------------------------------------------------------
// toFilterExpr / escapeTag / escapeText 单测
// ---------------------------------------------------------------------------

// TestToFilterExpr_Empty 两个字段都为空时返回空串，调用方据此跳过 WithFilterQuery。
func TestToFilterExpr_Empty(t *testing.T) {
	t.Parallel()
	if got := (RetrieveFilter{}).toFilterExpr(); got != "" {
		t.Fatalf("empty filter should yield empty expr, got %q", got)
	}
}

// TestToFilterExpr_StoredOnly 仅限定来源文档时，输出 @stored:{name} 精确匹配。
func TestToFilterExpr_StoredOnly(t *testing.T) {
	t.Parallel()
	got := RetrieveFilter{StoredName: "report.md"}.toFilterExpr()
	want := `@stored:{report\.md}`
	if got != want {
		t.Fatalf("stored only = %q, want %q", got, want)
	}
}

// TestToFilterExpr_HeadersOnly 仅限定章节时，输出 @headers:keyword 模糊匹配。
func TestToFilterExpr_HeadersOnly(t *testing.T) {
	t.Parallel()
	got := RetrieveFilter{Headers: "安装指南"}.toFilterExpr()
	want := "@headers:安装指南"
	if got != want {
		t.Fatalf("headers only = %q, want %q", got, want)
	}
}

// TestToFilterExpr_Both 两个字段同时设置时用空格拼接（AND 语义）。
func TestToFilterExpr_Both(t *testing.T) {
	t.Parallel()
	got := RetrieveFilter{StoredName: "doc.txt", Headers: "第一章"}.toFilterExpr()
	if !strings.HasPrefix(got, "@stored:{") || !strings.Contains(got, " @headers:") {
		t.Fatalf("both fields should produce AND expr, got %q", got)
	}
}

// TestEscapeTag 验证 TAG 值中的 RediSearch 保留字符被反斜杠转义。
func TestEscapeTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{"file.txt", `file\.txt`},
		{"a,b", `a\,b`},
		{"a{b}c", `a\{b\}c`},
		{"report (v2).md", `report \(v2\)\.md`},
		{"a:b@c#d", `a\:b\@c\#d`},
	}
	for _, tc := range cases {
		if got := escapeTag(tc.in); got != tc.want {
			t.Errorf("escapeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEscapeText 验证 TEXT 查询值中的 RediSearch 保留字符被反斜杠转义。
func TestEscapeText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"普通中文", "普通中文"},
		{"foo:bar", `foo\:bar`},
		{"a*b", `a\*b`},
		{"(hello)", `\(hello\)`},
		{`path\to`, `path\\to`},
		{"a|b", `a\|b`},
	}
	for _, tc := range cases {
		if got := escapeText(tc.in); got != tc.want {
			t.Errorf("escapeText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestToFilterExpr_SpecialCharsInStored 验证含保留字符的文档名经转义后不破坏查询语法。
func TestToFilterExpr_SpecialCharsInStored(t *testing.T) {
	t.Parallel()
	f := RetrieveFilter{StoredName: "my{doc}.txt"}
	got := f.toFilterExpr()
	if !strings.Contains(got, `my\{doc\}\.txt`) {
		t.Fatalf("special chars in stored should be escaped, got %q", got)
	}
}

// TestChunkLocator 校验从元数据解析 (stored, chunk)，兼容 int / string 两种 chunk 类型，
// 缺字段时返回 ok=false。
func TestChunkLocator(t *testing.T) {
	t.Parallel()

	// 索引期写入：chunk 为 int。
	if stored, idx, ok := chunkLocator(&schema.Document{MetaData: map[string]any{"stored": "doc.txt", "chunk": 3}}); !ok || stored != "doc.txt" || idx != 3 {
		t.Fatalf("int chunk locator = (%q,%d,%v), want (doc.txt,3,true)", stored, idx, ok)
	}
	// 检索期返回：chunk 为 string。
	if stored, idx, ok := chunkLocator(&schema.Document{MetaData: map[string]any{"stored": "doc.txt", "chunk": "5"}}); !ok || stored != "doc.txt" || idx != 5 {
		t.Fatalf("string chunk locator = (%q,%d,%v), want (doc.txt,5,true)", stored, idx, ok)
	}
	// 缺 stored（存量旧文档）→ 不可定位。
	if _, _, ok := chunkLocator(&schema.Document{MetaData: map[string]any{"chunk": 1}}); ok {
		t.Fatal("missing stored should be unlocatable")
	}
	// 缺 chunk → 不可定位。
	if _, _, ok := chunkLocator(&schema.Document{MetaData: map[string]any{"stored": "doc.txt"}}); ok {
		t.Fatal("missing chunk should be unlocatable")
	}
}

// TestExpandWithNeighbors_WindowZeroNoop 校验 window<=0 时原样返回、不改内容。
func TestExpandWithNeighbors_WindowZeroNoop(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{{Content: "C2", MetaData: map[string]any{"stored": "d", "chunk": "2"}}}
	got := expandWithNeighbors(docs, 0, func(string, int) string { return "X" })
	if len(got) != 1 || got[0].Content != "C2" {
		t.Fatalf("window=0 should be noop, got %v", got)
	}
}

// TestExpandWithNeighbors_Unlocatable 校验无法定位的块原样保留、不做扩展。
func TestExpandWithNeighbors_Unlocatable(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{{Content: "old", MetaData: map[string]any{}}}
	got := expandWithNeighbors(docs, 1, func(string, int) string { return "X" })
	if len(got) != 1 || got[0].Content != "old" {
		t.Fatalf("unlocatable doc should be kept as-is, got %v", got)
	}
}

// TestExpandWithNeighbors_MergeAndDedup 校验相邻命中的窗口按 chunk 序合并且不重复。
func TestExpandWithNeighbors_MergeAndDedup(t *testing.T) {
	t.Parallel()

	// 命中 chunk 2 与 chunk 3（同文档），window=1：窗口 [1,3] 与 [2,4] 重叠，应合并为一条。
	docs := []*schema.Document{
		{Content: "C2", MetaData: map[string]any{"stored": "d", "chunk": "2"}},
		{Content: "C3", MetaData: map[string]any{"stored": "d", "chunk": "3"}},
	}
	fetch := func(_ string, idx int) string { return fmt.Sprintf("N%d", idx) }

	got := expandWithNeighbors(docs, 1, fetch)
	if len(got) != 1 {
		t.Fatalf("overlapping windows should merge into 1 doc, got %d (%v)", len(got), got)
	}
	// 顺序为 chunk1(邻居) → chunk2(命中自身 Content) → chunk3(作为 chunk2 的邻居经 fetch 取回)，
	// chunk3 自身命中被去重跳过；生产环境 fetch 取回的正是 chunk_3 的存储正文。
	if want := "N1\nC2\nN3"; got[0].Content != want {
		t.Fatalf("merged content = %q, want %q", got[0].Content, want)
	}
}

// TestExpandWithNeighbors_Boundary 校验首块无前驱、邻居缺失时安全跳过。
func TestExpandWithNeighbors_Boundary(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{{Content: "C0", MetaData: map[string]any{"stored": "d", "chunk": "0"}}}
	// 邻居 chunk_1 不存在（fetch 返回空）→ 仅保留命中块自身。
	got := expandWithNeighbors(docs, 1, func(string, int) string { return "" })
	if len(got) != 1 || got[0].Content != "C0" {
		t.Fatalf("boundary expand = %v, want single C0", got)
	}
}
