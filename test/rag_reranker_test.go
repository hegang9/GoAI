package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	rag "GopherAI/internal/infrastructure/rag"

	"github.com/cloudwego/eino/schema"
)

// newDocs 构造测试用候选文档集合，内容用于校验精排后的顺序。
func newDocs(contents ...string) []*schema.Document {
	docs := make([]*schema.Document, 0, len(contents))
	for _, c := range contents {
		docs = append(docs, &schema.Document{Content: c, MetaData: map[string]any{}})
	}
	return docs
}

// TestHTTPReranker_Rerank_SortTruncateAndScore 校验精排：按分数降序重排、截断到 topN、回写 rerank_score。
func TestHTTPReranker_Rerank_SortTruncateAndScore(t *testing.T) {
	t.Parallel()

	// 模拟重排服务：故意返回乱序、且 index=1 分数最高，验证客户端会重新降序排序。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验鉴权头与请求体被正确发送。
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}
		resp := map[string]any{
			"results": []map[string]any{
				{"index": 0, "relevance_score": 0.20},
				{"index": 2, "relevance_score": 0.50},
				{"index": 1, "relevance_score": 0.90},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rr := rag.NewHTTPReranker(srv.URL, "test-key", "test-model")
	docs := newDocs("doc-A", "doc-B", "doc-C")

	// topN=2：应只保留分数最高的前两条（doc-B 0.90、doc-C 0.50）。
	got, err := rr.Rerank(context.Background(), "q", docs, 2)
	if err != nil {
		t.Fatalf("Rerank() err = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("Rerank() len = %d, want 2", len(got))
	}
	if got[0].Content != "doc-B" || got[1].Content != "doc-C" {
		t.Fatalf("Rerank() order = [%s, %s], want [doc-B, doc-C]", got[0].Content, got[1].Content)
	}
	if s, ok := got[0].MetaData["rerank_score"].(float64); !ok || s != 0.90 {
		t.Fatalf("Rerank() top rerank_score = %v (ok=%v), want 0.90", got[0].MetaData["rerank_score"], ok)
	}
}

// TestHTTPReranker_Rerank_EmptyDocs 校验空候选短路返回，不发起网络调用。
func TestHTTPReranker_Rerank_EmptyDocs(t *testing.T) {
	t.Parallel()

	// baseURL 指向不可达地址；若发生网络调用会报错，从而验证“空候选不调用”。
	rr := rag.NewHTTPReranker("http://127.0.0.1:0/never", "k", "m")
	got, err := rr.Rerank(context.Background(), "q", nil, 5)
	if err != nil {
		t.Fatalf("Rerank(empty) err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("Rerank(empty) len = %d, want 0", len(got))
	}
}

// TestHTTPReranker_Rerank_HTTPErrorStatus 校验非 2xx 返回 error（供上层降级）。
func TestHTTPReranker_Rerank_HTTPErrorStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	rr := rag.NewHTTPReranker(srv.URL, "k", "m")
	if _, err := rr.Rerank(context.Background(), "q", newDocs("a"), 1); err == nil {
		t.Fatalf("Rerank() err = nil, want non-nil on 500 status")
	}
}

// TestHTTPReranker_Rerank_BadJSON 校验响应体非法 JSON 时返回 error。
func TestHTTPReranker_Rerank_BadJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	rr := rag.NewHTTPReranker(srv.URL, "k", "m")
	if _, err := rr.Rerank(context.Background(), "q", newDocs("a"), 1); err == nil {
		t.Fatalf("Rerank() err = nil, want non-nil on invalid JSON")
	}
}

// TestHTTPReranker_Rerank_IgnoreOutOfRangeIndex 校验越界 index 被安全跳过，不 panic。
func TestHTTPReranker_Rerank_IgnoreOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"results": []map[string]any{
				{"index": 99, "relevance_score": 0.99}, // 越界，应跳过
				{"index": 0, "relevance_score": 0.10},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rr := rag.NewHTTPReranker(srv.URL, "k", "m")
	got, err := rr.Rerank(context.Background(), "q", newDocs("only"), 5)
	if err != nil {
		t.Fatalf("Rerank() err = %v, want nil", err)
	}
	if len(got) != 1 || got[0].Content != "only" {
		t.Fatalf("Rerank() = %v, want single [only]", got)
	}
}

// TestFilterByRerankScore 校验按精排最低分阈值过滤；阈值<=0 不过滤、缺分数保守保留。
func TestFilterByRerankScore(t *testing.T) {
	t.Parallel()

	docs := []*schema.Document{
		{Content: "高分", MetaData: map[string]any{"rerank_score": 0.80}}, // 保留
		{Content: "低分", MetaData: map[string]any{"rerank_score": 0.10}}, // 丢弃
		{Content: "无分", MetaData: map[string]any{}},                     // 缺分数，保守保留
	}

	got := rag.FilterByRerankScore(docs, 0.5)
	if len(got) != 2 {
		t.Fatalf("FilterByRerankScore() len = %d, want 2 (%v)", len(got), got)
	}
	for _, d := range got {
		if d.Content == "低分" {
			t.Fatalf("FilterByRerankScore() should have dropped low-score doc")
		}
	}

	// 阈值<=0：不过滤，原样返回。
	if all := rag.FilterByRerankScore(docs, 0); len(all) != len(docs) {
		t.Fatalf("FilterByRerankScore(0) len = %d, want %d", len(all), len(docs))
	}
}
