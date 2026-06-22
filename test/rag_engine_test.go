package test

import (
	"testing"

	rag "GopherAI/internal/infrastructure/rag"

	"github.com/cloudwego/eino/schema"
)

// TestParseDistance 校验多种类型的 distance 字段解析。
func TestParseDistance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		in    any
		want  float64
		wantK bool
	}{
		{name: "string", in: "0.25", want: 0.25, wantK: true},
		{name: "float64", in: 0.5, want: 0.5, wantK: true},
		{name: "float32", in: float32(0.75), want: 0.75, wantK: true},
		{name: "invalid string", in: "abc", wantK: false},
		{name: "nil", in: nil, wantK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := rag.ParseDistance(c.in)
			if ok != c.wantK {
				t.Fatalf("ParseDistance(%v) ok = %v, want %v", c.in, ok, c.wantK)
			}
			if ok && got != c.want {
				t.Fatalf("ParseDistance(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestFilterByDistance 校验按阈值过滤召回结果，且无法解析距离时保守保留。
func TestFilterByDistance(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{
		{Content: "近", MetaData: map[string]any{"distance": "0.10"}},  // 保留
		{Content: "远", MetaData: map[string]any{"distance": "0.90"}},  // 丢弃
		{Content: "临界", MetaData: map[string]any{"distance": "0.60"}}, // 等于阈值，保留
		{Content: "未知距离", MetaData: map[string]any{}},                 // 无距离，保守保留
	}

	got := rag.FilterByDistance(docs, 0.6)
	if len(got) != 3 {
		t.Fatalf("FilterByDistance() len = %d, want 3 (%v)", len(got), got)
	}
	for _, d := range got {
		if d.Content == "远" {
			t.Fatalf("FilterByDistance() should have dropped far doc")
		}
	}
}

// TestFilterByDistance_AllFilteredOut 校验全部超阈值时返回空，用于 RAG 路由跳过。
func TestFilterByDistance_AllFilteredOut(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{
		{Content: "a", MetaData: map[string]any{"distance": "0.95"}},
		{Content: "b", MetaData: map[string]any{"distance": "0.99"}},
	}
	if got := rag.FilterByDistance(docs, 0.6); len(got) != 0 {
		t.Fatalf("FilterByDistance() len = %d, want 0", len(got))
	}
}
