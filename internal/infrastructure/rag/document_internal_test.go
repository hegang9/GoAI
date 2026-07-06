package rag

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// TestHeaderPath 校验从 h1/h2/h3 拼出标题路径，缺失/空白层级被跳过。
func TestHeaderPath(t *testing.T) {
	t.Parallel()

	if got := headerPath(map[string]any{"h1": "一级", "h2": "二级", "h3": "三级"}); got != "一级 > 二级 > 三级" {
		t.Fatalf("full path = %q, want 一级 > 二级 > 三级", got)
	}
	// 仅 h1，且 h2 为空白应跳过。
	if got := headerPath(map[string]any{"h1": "一级", "h2": "  "}); got != "一级" {
		t.Fatalf("partial path = %q, want 一级", got)
	}
	if got := headerPath(nil); got != "" {
		t.Fatalf("nil meta path = %q, want empty", got)
	}
}

// TestInjectHeaders_MdAndPlain 校验块头标签注入：
//   - 有标题（Markdown）→ 前缀含「来源｜章节」，headers 元数据为标题路径；
//   - 无标题（纯文本）→ 前缀仅含「来源」，headers 元数据为空串。
func TestInjectHeaders_MdAndPlain(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{
		{Content: "正文一", MetaData: map[string]any{"h1": "概述", "h2": "背景"}},
		{Content: "正文二", MetaData: map[string]any{}},
	}
	injectHeaders(docs, "guide.md")

	if h, _ := docs[0].MetaData["headers"].(string); h != "概述 > 背景" {
		t.Fatalf("doc0 headers = %q, want 概述 > 背景", h)
	}
	if !strings.HasPrefix(docs[0].Content, "「来源：guide.md｜章节：概述 > 背景」\n") {
		t.Fatalf("doc0 content prefix wrong: %q", docs[0].Content)
	}
	if !strings.HasSuffix(docs[0].Content, "正文一") {
		t.Fatalf("doc0 original content lost: %q", docs[0].Content)
	}

	if h, _ := docs[1].MetaData["headers"].(string); h != "" {
		t.Fatalf("doc1 headers = %q, want empty", h)
	}
	if !strings.HasPrefix(docs[1].Content, "「来源：guide.md」\n") {
		t.Fatalf("doc1 content prefix wrong: %q", docs[1].Content)
	}
}
