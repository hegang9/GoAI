package rag

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestNewSplitter_RouteByExt 校验按扩展名路由到正确的切分器实现。
//   - .md / .markdown → MarkdownHeaderSplitter
//   - 其他扩展名      → RecursiveSplitter
func TestNewSplitter_RouteByExt(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		".md":       "MarkdownHeaderSplitter",
		".markdown": "MarkdownHeaderSplitter",
		".txt":      "RecursiveSplitter",
		".pdf":      "RecursiveSplitter",
		"":          "RecursiveSplitter",
	}
	for ext, want := range cases {
		sp, err := newSplitter(context.Background(), ext, 512, 64)
		if err != nil {
			t.Fatalf("newSplitter(%q) error = %v", ext, err)
		}
		typed, ok := sp.(interface{ GetType() string })
		if !ok {
			t.Fatalf("newSplitter(%q) result has no GetType()", ext)
		}
		if got := typed.GetType(); got != want {
			t.Fatalf("newSplitter(%q) type = %q, want %q", ext, got, want)
		}
	}
}

// TestNormalizeSplitParams 校验非法分块参数被收敛为合法正步长，避免切分器构造失败。
func TestNormalizeSplitParams(t *testing.T) {
	t.Parallel()

	// chunkSize<=0 回退默认值；overlap<0 回退默认；overlap>=chunkSize 收敛为一半。
	if cs, ov := normalizeSplitParams(0, -1); cs != defaultChunkSize || ov != defaultChunkOverlap {
		t.Fatalf("normalizeSplitParams(0,-1) = (%d,%d), want (%d,%d)", cs, ov, defaultChunkSize, defaultChunkOverlap)
	}
	if cs, ov := normalizeSplitParams(10, 20); cs != 10 || ov != 5 {
		t.Fatalf("normalizeSplitParams(10,20) = (%d,%d), want (10,5)", cs, ov)
	}
}

// TestLoadByFixedWindow_Fallback 校验定长滑窗兜底产出非空且 ID/元数据契约正确。
//
// 这是切分器创建或执行失败时的降级路径，必须保证索引流程不中断、产出可用文档块。
func TestLoadByFixedWindow_Fallback(t *testing.T) {
	t.Parallel()

	docs, err := loadByFixedWindow(strings.Repeat("A", 30), "fallback.txt", 10, 0)
	if err != nil {
		t.Fatalf("loadByFixedWindow() error = %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("loadByFixedWindow() chunks = %d, want 3", len(docs))
	}
	for i, d := range docs {
		if want := fmt.Sprintf("chunk_%d", i); d.ID != want {
			t.Fatalf("doc[%d] ID = %q, want %q", i, d.ID, want)
		}
		if src, _ := d.MetaData["source"].(string); src != "fallback.txt" {
			t.Fatalf("doc[%d] source = %v, want fallback.txt", i, d.MetaData["source"])
		}
	}

	// 空内容应返回错误，避免建立空索引。
	if _, err := loadByFixedWindow("   ", "blank.txt", 10, 0); err == nil {
		t.Fatal("loadByFixedWindow(blank) error = nil, want error")
	}
}
