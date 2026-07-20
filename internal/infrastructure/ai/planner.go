package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"GopherAI/internal/domain/chat"
	"GopherAI/internal/infrastructure/storage"
	"GopherAI/pkg/logger"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TurnPlan 是单轮检索决策计划，由 planner 产出。
//
// 注意：不含工具决策——工具默认开放给主模型 native tool calling，
// 是否调用工具由模型在生成中自主决定，不进入本计划。
type TurnPlan struct {
	// NeedRetrieval 是否需要检索用户私有知识库。
	NeedRetrieval bool
	// RetrievalQuery 检索用查询句；NeedRetrieval=true 时应语义自包含（多轮指代已改写）。
	RetrievalQuery string
	// DocFilter 文档范围过滤（storedName / headers）；仅用户明确点名时填写。
	DocFilter chat.RAGFilter
	// Confidence 决策置信度："high" | "medium" | "low"；low 时系统会跳过检索。
	Confidence string
	// Reason 简短中文决策理由，便于日志排查。
	Reason string
	// Source 决策来源："rule" | "planner" | "fallback" | "disabled"。
	Source string
}

// Planner 是轻量检索决策器：从最近若干轮消息产出 TurnPlan。
//
// 逻辑迁移自 RAGModel.rewriteQuery + parseFilterIntent + "是否检索"决策，
// 但职责收窄为只决定检索，不再解析工具意图。
type Planner struct {
	// llm planner 专用轻量模型，用于产出结构化 TurnPlan。
	llm einomodel.ToolCallingChatModel
	// historyWindow 决策时纳入 prompt 的最近消息条数。
	historyWindow int
	// timeoutMs planner LLM 调用超时（毫秒），超时则降级为不检索。
	timeoutMs int
}

// NewPlanner 创建 planner 实例。modelName/baseURL/apiKey 为 planner 专用轻量模型配置。
//
// historyWindow / timeoutMs <=0 时分别回退为 8 / 1200。
func NewPlanner(ctx context.Context, modelName, baseURL, apiKey string, historyWindow, timeoutMs int) (*Planner, error) {
	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  apiKey,
	})
	if err != nil {
		logger.Error("NewPlanner create model failed", "err", err)
		return nil, fmt.Errorf("create planner model failed: %v", err)
	}
	if historyWindow <= 0 {
		historyWindow = 8
	}
	if timeoutMs <= 0 {
		timeoutMs = 1200
	}
	logger.Info("NewPlanner success", "model", modelName, "historyWindow", historyWindow, "timeoutMs", timeoutMs)
	return &Planner{llm: llm, historyWindow: historyWindow, timeoutMs: timeoutMs}, nil
}

// Plan 产出单轮检索决策计划。三层门禁：规则门禁 → planner → fallback。
//
// explicitFilter 为用户显式指定的文档范围（来自前端 storedName/headers，
// 经 ctx 传入），优先级最高：非空时直接判定 need_retrieval=true，跳过 planner。
func (p *Planner) Plan(ctx context.Context, accountNo string, messages []chat.Message, explicitFilter chat.RAGFilter) TurnPlan {
	// 规则门禁 1：账号无文档 → 不检索，零 LLM 开销。
	if !storage.HasUserDocs(accountNo) {
		return TurnPlan{NeedRetrieval: false, Source: "rule", Confidence: "high", Reason: "账号无文档，跳过检索"}
	}

	// 规则门禁 2：用户显式指定文档范围 → 必检索，query 用最后一条用户消息。
	if !explicitFilter.IsEmpty() {
		return TurnPlan{
			NeedRetrieval:  true,
			RetrievalQuery: lastUserMessage(messages),
			DocFilter:      explicitFilter,
			Source:         "rule",
			Confidence:     "high",
			Reason:         "用户显式指定文档范围",
		}
	}

	// planner 层：LLM 决策，失败时降级。
	return p.planWithLLM(ctx, accountNo, messages)
}

// planWithLLM 调用轻量模型产出结构化检索计划，超时或解析失败时降级为不检索。
func (p *Planner) planWithLLM(ctx context.Context, accountNo string, messages []chat.Message) TurnPlan {
	planCtx, cancel := context.WithTimeout(ctx, time.Duration(p.timeoutMs)*time.Millisecond)
	defer cancel()

	// 调用 LLM 判断（带计时，供观测 latency.plan_ms）
	planStart := time.Now()
	resp, err := p.llm.Generate(planCtx, []*schema.Message{
		{Role: schema.User, Content: p.buildPlannerPrompt(messages)},
	})
	planMs := time.Since(planStart).Milliseconds()
	if err != nil {
		logger.Warn("Planner LLM failed, fallback to no retrieval",
			"accountNo", accountNo,
			"fallback.reason", "llm_error",
			"latency.plan_ms", planMs,
			"err", err)
		return TurnPlan{NeedRetrieval: false, Source: "fallback", Confidence: "low", Reason: "planner llm调用失败"}
	}

	// 容错解析 planner llm 解析输出
	plan, ok := parsePlannerJSON(resp.Content)
	// TODO：判断这里是否需要重试，防止一次输出不合法就回退普通聊天
	if !ok {
		logger.Warn("Planner JSON parse failed, fallback to no retrieval",
			"accountNo", accountNo,
			"fallback.reason", "json_parse_error",
			"latency.plan_ms", planMs,
			"raw", resp.Content)
		return TurnPlan{NeedRetrieval: false, Source: "fallback", Confidence: "low", Reason: "planner 输出非法"}
	}
	plan.Source = "planner"

	// 低置信度降级为不检索：宁可漏检索也不要错检索污染回答。
	if plan.Confidence == "low" {
		plan.NeedRetrieval = false
		plan.Source = "fallback"
	}
	logger.Info("Planner decision",
		"accountNo", accountNo,
		"plan.need_retrieval", plan.NeedRetrieval,
		"plan.confidence", plan.Confidence,
		"plan.source", plan.Source,
		"plan.reason", plan.Reason,
		"latency.plan_ms", planMs)
	return plan
}

// buildPlannerPrompt 构造决策提示词，携带最近 historyWindow 轮历史。
func (p *Planner) buildPlannerPrompt(messages []chat.Message) string {
	start := len(messages) - p.historyWindow
	if start < 0 {
		start = 0
	}
	var history strings.Builder
	for _, m := range messages[start:] {
		role := "用户"
		if !m.IsUser {
			role = "助手"
		}
		history.WriteString(fmt.Sprintf("%s：%s\n", role, m.Content))
	}
	return fmt.Sprintf(`你是检索决策器。根据多轮对话历史，判断"是否需要检索用户私有知识库"，并产出结构化检索计划。

判定规则：
- 用户问题明显依赖其私有文档（制度、报告、手册等）→ need_retrieval=true
- 明显闲聊、通用知识、创意写作 → need_retrieval=false
- 多轮指代追问时，把最后一句改写为语义自包含的检索 query
- doc_filter 仅在用户明确点名文档名或章节时填写，不要臆测

输出格式（只输出一个 JSON 对象，不要解释，不要 markdown 代码块）：
{
  "need_retrieval": true|false,
  "retrieval_query": "string",
  "doc_filter": {
    "storedName": "string",
    "headers": "string"
  },
  "confidence": "high"|"medium"|"low",
  "reason": "string"
}

字段说明：
- need_retrieval (bool)：是否需要检索用户私有知识库。true=需要；false=不需要。
- retrieval_query (string)：检索用查询句。need_retrieval=true 时必填，且须语义自包含（多轮指代要改写完整）；false 时必须为 ""。
- doc_filter.storedName (string)：来源文档文件名（含扩展名，如 report.md）。仅当用户明确点名文档时填写，否则 ""。
- doc_filter.headers (string)：章节路径关键字（如"安装指南"）。仅当用户明确点名章节时填写，否则 ""。
- confidence ("high"|"medium"|"low")：对本决策的确信程度。不确定时用 low（系统会跳过检索）。
- reason (string)：简短中文理由，一两句即可。

返回示例：
{"need_retrieval":true,"retrieval_query":"员工手册中关于年假申请的流程","doc_filter":{"storedName":"员工手册.md","headers":"年假"},"confidence":"high","reason":"用户询问私有制度文档中的具体流程"}

对话历史：
%s
输出 JSON：`, history.String())
}

// parsePlannerJSON 容错解析 planner 输出，提取首个 { 到末个 } 的子串，最终返回标准TurnPlan结构体。
// 沿用 RAGModel.parseFilterIntent 的容错策略，兼容 LLM 输出多余文本。
func parsePlannerJSON(raw string) (TurnPlan, bool) {
	body := strings.TrimSpace(raw)
	lo := strings.Index(body, "{")
	hi := strings.LastIndex(body, "}")
	if lo < 0 || hi <= lo {
		return TurnPlan{}, false
	}
	var parsed struct {
		NeedRetrieval  bool   `json:"need_retrieval"`
		RetrievalQuery string `json:"retrieval_query"`
		DocFilter      struct {
			StoredName string `json:"storedName"`
			Headers    string `json:"headers"`
		} `json:"doc_filter"`
		Confidence string `json:"confidence"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(body[lo:hi+1]), &parsed); err != nil {
		return TurnPlan{}, false
	}
	return TurnPlan{
		NeedRetrieval:  parsed.NeedRetrieval,
		RetrievalQuery: parsed.RetrievalQuery,
		DocFilter:      chat.RAGFilter{StoredName: parsed.DocFilter.StoredName, Headers: parsed.DocFilter.Headers},
		Confidence:     parsed.Confidence,
		Reason:         parsed.Reason,
	}, true
}

// lastUserMessage 取最后一条用户消息内容，用于规则门禁显式过滤场景的检索 query。
// 找不到用户消息时回退到最后一条消息内容，保证检索仍可进行。
func lastUserMessage(messages []chat.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].IsUser {
			return messages[i].Content
		}
	}
	if len(messages) > 0 {
		return messages[len(messages)-1].Content
	}
	return ""
}
