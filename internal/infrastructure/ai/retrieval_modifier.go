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
// 设计约束：检索必须在进入 ADK Agent 循环前完成一次。循环中途最后一条消息可能是
// 工具结果；重复检索既昂贵，也会错误地修改工具调用结果。
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

// Modify 按 TurnPlan 决定是否检索并增强最后一条用户消息，返回可直接交给 ADK Agent 的消息。
//
// 流程：
//  1. planner 为 nil（disabled）→ 透传原消息，纯生成
//  2. planner.Plan 产出 TurnPlan（含规则门禁短路）
//  3. need_retrieval=false → 透传原消息
//  4. need_retrieval=true → 用 plan.RetrievalQuery 与 plan.DocFilter 检索，
//     命中则替换最后一条用户消息为增强 prompt；检索失败或无命中则透传原消息
func (r *RetrievalModifier) Modify(ctx context.Context, history []chat.Message) []*schema.Message {
	return r.ModifyPrepared(ctx, history, toSchemaMessages(history))
}

// ModifyPrepared 与 Modify 使用相同检索流程，但允许调用方传入已经过摘要/分层记忆恢复的模型消息。
// Planner 仍接收原始领域历史以保持检索决策信息完整，最终 RAG prompt 则写入有界上下文的最后一条用户消息。
func (r *RetrievalModifier) ModifyPrepared(ctx context.Context, history []chat.Message, prepared []*schema.Message) []*schema.Message {
	messages := prepared
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
