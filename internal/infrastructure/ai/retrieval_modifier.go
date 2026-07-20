package ai

import (
	"context"
	"time"

	"GopherAI/internal/domain/chat"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/pkg/logger"

	"github.com/cloudwego/eino/schema"
)

// RetrievalModifier 按 TurnPlan 对输入消息做一次性检索增强（pre-generation 上下文准备）。
//
// 设计约束：检索是 pre-generation 的上下文准备，必须在进入 ReAct 循环前完成一次，
// 不能放进 ReAct 的 MessageModifier——后者在每轮模型调用前都会被触发，且循环中途
// 「最后一条消息」是工具结果而非用户消息，重复检索既昂贵又会错误地修改工具调用结果。
// 因此本类型不实现 react.MessageModifier，而是一个在 agent 调用前执行的一次性变换。
//
// 职责去重：planner 已产出 RetrievalQuery 与 DocFilter，这里不再做 query 改写与
// filter 解析，直接用 planner 输出执行检索（取代旧 RAGModel.buildRAGMessages）。
type RetrievalModifier struct {
	planner   *Planner
	engine    *raginfra.Engine
	accountNo string
}

// NewRetrievalModifier 创建检索增强器。planner 为 nil 时退化为透传（纯生成）。
func NewRetrievalModifier(planner *Planner, engine *raginfra.Engine, accountNo string) *RetrievalModifier {
	return &RetrievalModifier{planner: planner, engine: engine, accountNo: accountNo}
}

// Modify 按 TurnPlan 决定是否检索并增强最后一条用户消息，返回可直接喂给 ReAct Agent 的消息。
//
// 流程：
//  1. planner 为 nil（disabled）→ 透传原消息，纯生成
//  2. planner.Plan 产出 TurnPlan（含规则门禁短路）
//  3. need_retrieval=false → 透传原消息
//  4. need_retrieval=true → 用 plan.RetrievalQuery 与 plan.DocFilter 检索，
//     命中则替换最后一条用户消息为增强 prompt；检索失败或无命中则透传原消息
func (r *RetrievalModifier) Modify(ctx context.Context, history []chat.Message) []*schema.Message {
	messages := toSchemaMessages(history)
	if len(messages) == 0 || r.planner == nil {
		return messages
	}

	// 显式过滤（前端 storedName/headers，经 ctx 传入）优先级最高，由 planner 内部规则门禁处理。
	plan := r.planner.Plan(ctx, r.accountNo, history, chat.RetrieveFilterFromCtx(ctx))
	if !plan.NeedRetrieval {
		return messages
	}

	engineFilter := raginfra.RetrieveFilter{
		StoredName: plan.DocFilter.StoredName,
		Headers:    plan.DocFilter.Headers,
	}
	// 实际执行检索（带计时，供观测 latency.retrieve_ms）
	retrieveStart := time.Now()
	prompt, hasContext, hitCount, err := r.engine.Retrieve(ctx, r.accountNo, plan.RetrievalQuery, engineFilter)
	retrieveMs := time.Since(retrieveStart).Milliseconds()
	if err != nil {
		logger.Warn("RetrievalModifier retrieve failed, fallback to plain",
			"accountNo", r.accountNo,
			"plan.source", plan.Source,
			"fallback.reason", "retrieve_error",
			"latency.retrieve_ms", retrieveMs,
			"err", err)
		return messages
	}
	if !hasContext {
		// 无相关内容：不注入参考文档，避免污染普通对话。
		logger.Info("RetrievalModifier no relevant docs",
			"accountNo", r.accountNo,
			"plan.source", plan.Source,
			"plan.confidence", plan.Confidence,
			"retrieval.hit_count", 0,
			"latency.retrieve_ms", retrieveMs)
		return messages
	}

	logger.Info("RetrievalModifier retrieve enhanced",
		"accountNo", r.accountNo,
		"plan.source", plan.Source,
		"plan.confidence", plan.Confidence,
		"retrieval.hit_count", hitCount,
		"latency.retrieve_ms", retrieveMs)

	out := make([]*schema.Message, len(messages))
	copy(out, messages)
	out[len(out)-1] = &schema.Message{Role: schema.User, Content: prompt}
	return out
}
