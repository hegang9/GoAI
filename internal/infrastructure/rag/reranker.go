// Package rag 检索增强适配层：本文件提供“精排（reranker）”能力。
//
// 设计背景：向量召回（双塔编码）擅长低成本放大候选集，但对“语义相关但措辞不同”
// “一词多义”等场景排序不够精准。精排阶段用交叉编码（cross-encoder）对 query-doc
// 做联合打分，提升最终 TopN 的命中率。
//
// Reranker 接口定义在基础设施层（而非领域层），原因：其入参/返回值复用 eino 的
// schema.Document，与现有检索结果零转换；而领域层（internal/domain）受架构约束
// 禁止依赖 eino 等具体框架。接口隔离仍保留可插拔性，便于切换 Ark / BGE-reranker /
// Cohere 等实现。
package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"GopherAI/pkg/logger"

	"github.com/cloudwego/eino/schema"
)

// rerankHTTPTimeout 精排服务调用的默认超时，避免外部服务抖动拖垮 RAG 链路。
const rerankHTTPTimeout = 10 * time.Second

// Reranker 定义“检索结果精排”端口：对候选文档按与 query 的相关性重新打分排序。
//
// 之所以放在基础设施层：返回值复用 eino 的 schema.Document，领域层禁止依赖 eino。
type Reranker interface {
	// Rerank 返回按相关性降序排列的文档（已截断到 topN）。
	// 每个返回文档的 MetaData["rerank_score"] 记录精排分数（越大越相关）。
	Rerank(ctx context.Context, query string, docs []*schema.Document, topN int) ([]*schema.Document, error)
}

// HTTPReranker 通过外部重排服务（OpenAI / Cohere 风格的 rerank API）对候选文档精排。
//
// 请求体 / 响应体字段按主流 rerank API 约定实现；若对接的服务字段不同，可在此调整。
type HTTPReranker struct {
	// baseURL 重排服务完整地址（含 path），如 https://.../rerank。
	baseURL string
	// apiKey 重排服务鉴权 Key，以 Bearer 方式放入 Authorization 头。
	apiKey string
	// model 重排模型名称。
	model string
	// client 复用的 HTTP 客户端，带超时控制。
	client *http.Client
}

// NewHTTPReranker 创建重排器；baseURL/apiKey/model 由统一配置注入。
func NewHTTPReranker(baseURL, apiKey, model string) *HTTPReranker {
	return &HTTPReranker{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: rerankHTTPTimeout},
	}
}

// 编译期断言：HTTPReranker 必须满足精排端口。
var _ Reranker = (*HTTPReranker)(nil)

// rerankReq 是发送给重排服务的请求体。
type rerankReq struct {
	// Model 重排模型名。
	Model string `json:"model"`
	// Query 用户查询。
	Query string `json:"query"`
	// Documents 候选文档文本列表，下标与返回结果的 Index 对应。
	Documents []string `json:"documents"`
	// TopN 期望返回的精排结果数量。
	TopN int `json:"top_n"`
}

// rerankResp 是重排服务的响应体。
type rerankResp struct {
	Results []struct {
		// Index 对应请求 Documents 的下标。
		Index int `json:"index"`
		// RelevanceScore 相关性分数，越大越相关。
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

// Rerank 调用重排服务并返回按分数降序、截断到 topN 的文档。
//
// 失败时返回 error，由调用方（Engine.Retrieve）决定降级策略（回退到向量排序）。
func (r *HTTPReranker) Rerank(ctx context.Context, query string, docs []*schema.Document, topN int) ([]*schema.Document, error) {
	// 空候选直接返回，避免无意义的网络调用。
	if len(docs) == 0 {
		return docs, nil
	}
	if topN <= 0 || topN > len(docs) {
		topN = len(docs)
	}

	start := time.Now()
	contents := make([]string, len(docs))
	for i, d := range docs {
		contents[i] = d.Content
	}

	body, err := json.Marshal(rerankReq{Model: r.model, Query: query, Documents: contents, TopN: topN})
	if err != nil {
		logger.Error("rerank marshal request failed", "err", err)
		return nil, fmt.Errorf("marshal rerank request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL, bytes.NewReader(body))
	if err != nil {
		logger.Error("rerank build request failed", "err", err)
		return nil, fmt.Errorf("build rerank request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Error("rerank request failed", "err", err)
		return nil, fmt.Errorf("rerank request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 非 2xx 视为失败：读取部分响应体辅助排查，并触发上层降级。
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		logger.Error("rerank unexpected status", "status", resp.StatusCode, "body", string(snippet))
		return nil, fmt.Errorf("rerank unexpected status: %d", resp.StatusCode)
	}

	var rr rerankResp
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		logger.Error("rerank decode response failed", "err", err)
		return nil, fmt.Errorf("decode rerank response failed: %w", err)
	}

	// 按分数降序稳定排序，确保结果有序（即使服务端未保证）。
	sort.SliceStable(rr.Results, func(i, j int) bool {
		return rr.Results[i].RelevanceScore > rr.Results[j].RelevanceScore
	})

	out := make([]*schema.Document, 0, len(rr.Results))
	for _, item := range rr.Results {
		// 防御非法下标，避免越界 panic。
		if item.Index < 0 || item.Index >= len(docs) {
			logger.Warn("rerank result index out of range", "index", item.Index, "candidates", len(docs))
			continue
		}
		d := docs[item.Index]
		if d.MetaData == nil {
			d.MetaData = map[string]any{}
		}
		// 回写精排分数，供后续阈值过滤与 BuildPrompt / 日志观测使用。
		d.MetaData["rerank_score"] = item.RelevanceScore
		out = append(out, d)
		if len(out) >= topN {
			break
		}
	}

	logger.Info("rerank success",
		"candidates", len(docs),
		"returned", len(out),
		"topN", topN,
		"cost", time.Since(start))
	return out, nil
}
