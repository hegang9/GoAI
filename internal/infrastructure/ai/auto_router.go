package ai

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"GopherAI/internal/domain/chat"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/pkg/logger"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	mcpcli "github.com/mark3labs/mcp-go/client"
)

// autoSystemPrompt 作为 ReAct Agent 的人设/路由说明：工具默认开放，由模型在生成中自主决定是否调用。
const autoSystemPrompt = `你是一个智能助手，可在需要时调用工具获取实时信息（如天气）。
请根据用户问题自行决定是否调用工具；不需要工具时直接用自然语言回答。`

// AutoRouterModel 是统一自动编排模型，实现 domain/chat.Model 端口。
//
// Phase 3 形态：ReAct Agent（ToolCallingModel + 默认 MCP 工具集 + 系统提示 MessageModifier）
// 叠加 RetrievalModifier（进入 Agent 前的一次性检索增强）。
// RAGModel/MCPModel 已退役，工厂只保留 "auto"。
//
// 单次请求主路径：
//  1. ensureAgent —— 懒建 ReAct Agent（含可选 MCP 工具）
//  2. retrieval.Modify —— Planner 决策 + 一次性检索增强
//  3. agent.Generate/Stream —— 模型自主决定是否调工具并产出回复
//
// 执行矩阵（系统只分叉 need_retrieval，工具是否调用由模型自主决定）：
//   - need_retrieval=false → 直接生成（可能调工具）
//   - need_retrieval=true  → 先检索增强 prompt，再生成（可能调工具）
//
// per-account 构造：检索与工具集都依赖 accountNo，与旧 RAGModel/MCPModel 一致。
type AutoRouterModel struct {
	// retrieval 进入 Agent 前的一次性检索增强；持有 planner + engine + accountNo。
	retrieval *RetrievalModifier
	// llm 主回答模型（具备 native function calling 能力）。
	llm einomodel.ToolCallingChatModel
	// mcpBaseURL MCP Server 地址；为空时不注入工具，退化为纯生成（+ 可选检索）。
	mcpBaseURL string

	// mu 保护懒初始化的并发安全。
	mu sync.Mutex
	// agent 懒构建的 ReAct Agent；为 nil 表示尚未完成初始化。
	agent *react.Agent
	// mcpClient 懒连接的 MCP 客户端，随 agent 一同初始化，供关闭时释放。
	mcpClient *mcpcli.Client
}

// NewAutoRouterModel 创建自动编排模型实例。
//
// 仅初始化 LLM 与 RetrievalModifier，不连接 MCP——工具侧在首次 Generate/Stream 时懒加载。
// planner 为 nil 时（plannerConfig.enabled=false）退化为「总是不检索的纯生成」，
// 行为等价于普通对话模型，保证 planner 不可用时链路不中断。
func NewAutoRouterModel(ctx context.Context, accountNo, modelName, baseURL, apiKey, mcpBaseURL string, planner *Planner, engine *raginfra.Engine) (*AutoRouterModel, error) {
	// 1) 创建主回答模型（具备 tool calling）
	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  apiKey,
	})
	if err != nil {
		logger.Error("NewAutoRouterModel create model failed", "accountNo", accountNo, "err", err)
		return nil, fmt.Errorf("create auto router model failed: %v", err)
	}
	logger.Info("NewAutoRouterModel success",
		"accountNo", accountNo, "model", modelName,
		"plannerEnabled", planner != nil, "mcpBaseURL", mcpBaseURL)

	// 2) 装配检索增强器；Agent/MCP 留待 ensureAgent 懒建
	return &AutoRouterModel{
		retrieval:  NewRetrievalModifier(planner, engine, accountNo),
		llm:        llm,
		mcpBaseURL: mcpBaseURL,
	}, nil
}

// 编译期断言：AutoRouterModel 必须满足领域模型端口。
var _ chat.Model = (*AutoRouterModel)(nil)

// ensureAgent 懒加载 ReAct Agent：首次调用时连接 MCP Server、拉取并转换工具、构建带工具循环的 Agent。
//
// 降级策略（对齐计划「MCP 工具临时不可用时降级为纯生成」）：
//   - mcpBaseURL 为空 → 直接构建无工具 Agent 并缓存（无 MCP 可重试，纯生成形态稳定）。
//   - MCP 连接或工具拉取失败 → 本次构建无工具 Agent 供调用，但不缓存 agent，
//     下次调用会重新尝试连接 MCP，避免 Server 临时不可用导致永久丢失工具能力。
//   - 成功构建带工具 Agent → 缓存复用，避免重复连接。
func (m *AutoRouterModel) ensureAgent(ctx context.Context) (*react.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 已缓存则直接复用（成功路径或「无 MCP 配置」的稳定纯生成形态）
	if m.agent != nil {
		return m.agent, nil
	}

	// 1) 懒连 MCP 并转换工具；mcpBaseURL 为空时 tools 为空且无错误
	tools, mcpReady, err := m.loadTools(ctx)
	if err != nil {
		// 2a) MCP 临时失败：本次无工具生成，不写入 m.agent，下次可重试
		agent, berr := m.buildAgent(ctx, nil)
		if berr != nil {
			return nil, berr
		}
		logger.Warn("AutoRouterModel mcp unavailable, build no-tool agent for this call",
			"accountNo", m.retrieval.accountNo,
			"fallback.reason", "mcp_unavailable",
			"err", err)
		return agent, nil
	}

	// 2b) 按工具集组装 ReAct Agent（tools 可为空）
	agent, err := m.buildAgent(ctx, tools)
	if err != nil {
		// 已连上 MCP 但 Agent 构建失败：释放客户端，避免泄漏
		if mcpReady {
			m.closeMCP()
		}
		logger.Error("AutoRouterModel create react agent failed",
			"accountNo", m.retrieval.accountNo,
			"fallback.reason", "agent_build_error",
			"err", err)
		return nil, fmt.Errorf("create auto router agent failed: %v", err)
	}

	// 3) 成功则缓存，供后续请求复用；记录可用工具名供观测
	m.agent = agent
	logger.Info("AutoRouterModel ensureAgent success",
		"accountNo", m.retrieval.accountNo,
		"tools.count", len(tools),
		"tools.names", toolNames(ctx, tools))
	return agent, nil
}

// loadTools 懒连接 MCP Server 并转换工具。返回 (tools, mcpReady, err)。
//
// mcpBaseURL 为空时返回空工具集且无错误（该账号不配置 MCP，纯生成形态）。
// mcpReady=true 表示已建立 MCP 连接，调用方需在失败路径上负责关闭。
func (m *AutoRouterModel) loadTools(ctx context.Context) ([]tool.BaseTool, bool, error) {
	// 未配置 MCP：无工具可加载，不算失败
	if m.mcpBaseURL == "" {
		return nil, false, nil
	}

	// 1) 连接 MCP Server
	mcpClient, err := newMCPClient(ctx, m.mcpBaseURL)
	if err != nil {
		return nil, false, fmt.Errorf("connect mcp failed: %w", err)
	}

	// 2) 拉取并转换为 Eino 工具；失败则立刻关闭本次连接
	tools, err := newMCPTools(ctx, mcpClient)
	if err != nil {
		_ = mcpClient.Close()
		return nil, false, fmt.Errorf("build mcp tools failed: %w", err)
	}

	// 3) 挂到实例上，供 Close / 构建失败回滚时释放
	m.mcpClient = mcpClient
	return tools, true, nil
}

// buildAgent 用 ReAct 组装 Agent：ToolCallingModel + 默认工具集 + 系统提示 MessageModifier。
//
// tools 为空时不注入 ToolsConfig，Agent 退化为纯生成（仍可承载检索增强后的输入）。
// MessageModifier 只做幂等的系统提示注入——它在 ReAct 每轮模型调用前都会被触发，
// 因此不能承载检索（检索是 pre-generation，已由 RetrievalModifier 在 Agent 调用前完成）。
func (m *AutoRouterModel) buildAgent(ctx context.Context, tools []tool.BaseTool) (*react.Agent, error) {
	cfg := &react.AgentConfig{
		ToolCallingModel: m.llm,
		// 每轮模型调用前注入系统提示；必须保持幂等，勿在此做检索
		MessageModifier: func(_ context.Context, in []*schema.Message) []*schema.Message {
			return append([]*schema.Message{schema.SystemMessage(autoSystemPrompt)}, in...)
		},
	}
	// 有工具才挂 ToolsNode；无工具时仍可直接自然语言生成
	if len(tools) > 0 {
		cfg.ToolsConfig = compose.ToolsNodeConfig{Tools: tools}
	}
	return react.NewAgent(ctx, cfg)
}

// toolNames 收集工具集的可读名称，供观测日志记录可用工具。
// Info 调用失败时跳过该工具，不影响其余工具名收集。
func toolNames(ctx context.Context, tools []tool.BaseTool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		names = append(names, info.Name)
	}
	return names
}

// closeMCP 关闭并清空懒连接的 MCP 客户端，调用方需持锁。
func (m *AutoRouterModel) closeMCP() {
	if m.mcpClient != nil {
		if err := m.mcpClient.Close(); err != nil {
			logger.Warn("AutoRouterModel close mcp client failed",
				"accountNo", m.retrieval.accountNo, "err", err)
		}
		m.mcpClient = nil
	}
}

// Generate 同步生成：先一次性检索增强，再由 ReAct Agent 完成生成（可能多轮工具调用）。
func (m *AutoRouterModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	// 步骤 1：确保 ReAct Agent 可用（含 MCP 懒加载 / 降级）
	agent, err := m.ensureAgent(ctx)
	if err != nil {
		logger.Error("AutoRouterModel Generate ensureAgent failed",
			"accountNo", m.retrieval.accountNo, "err", err)
		return "", fmt.Errorf("auto router ensure agent failed: %v", err)
	}

	// 步骤 2：进入 Agent 前做一次性检索增强（Planner 决策 + 可选 RAG）
	messages := m.retrieval.Modify(ctx, history)

	// 步骤 3：ReAct 生成（模型可按需 native tool calling），带计时供观测 latency.answer_ms
	answerStart := time.Now()
	out, err := agent.Generate(ctx, messages)
	if err != nil {
		logger.Error("AutoRouterModel Generate failed",
			"accountNo", m.retrieval.accountNo,
			"latency.answer_ms", time.Since(answerStart).Milliseconds(),
			"err", err)
		return "", fmt.Errorf("auto router generate failed: %v", err)
	}
	logger.Info("AutoRouterModel Generate done",
		"accountNo", m.retrieval.accountNo,
		"latency.answer_ms", time.Since(answerStart).Milliseconds())
	return out.Content, nil
}

// Stream 流式生成：先一次性检索增强，再由 ReAct Agent 流式产出最终回复。
// 工具调用阶段不向前端分片，与旧 MCPModel 行为一致。
func (m *AutoRouterModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	// 步骤 1：确保 ReAct Agent 可用（含 MCP 懒加载 / 降级）
	agent, err := m.ensureAgent(ctx)
	if err != nil {
		logger.Error("AutoRouterModel Stream ensureAgent failed",
			"accountNo", m.retrieval.accountNo, "err", err)
		return "", fmt.Errorf("auto router ensure agent failed: %v", err)
	}

	// 步骤 2：进入 Agent 前做一次性检索增强（Planner 决策 + 可选 RAG）
	messages := m.retrieval.Modify(ctx, history)

	// 步骤 3：开启 ReAct 流式输出（带计时供观测 latency.answer_ms，覆盖到首字+全程）
	answerStart := time.Now()
	stream, err := agent.Stream(ctx, messages)
	if err != nil {
		logger.Error("AutoRouterModel Stream start failed",
			"accountNo", m.retrieval.accountNo,
			"latency.answer_ms", time.Since(answerStart).Milliseconds(),
			"err", err)
		return "", fmt.Errorf("auto router stream failed: %v", err)
	}
	defer stream.Close()

	// 步骤 4：聚合并回调有内容的最终回复分片
	var full strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("AutoRouterModel Stream recv failed",
				"accountNo", m.retrieval.accountNo,
				"latency.answer_ms", time.Since(answerStart).Milliseconds(),
				"err", err)
			return "", fmt.Errorf("auto router stream recv failed: %v", err)
		}
		// 工具调用中间态 Content 为空，自然跳过；只把最终自然语言分片推给前端
		if len(msg.Content) > 0 {
			full.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	logger.Info("AutoRouterModel Stream done",
		"accountNo", m.retrieval.accountNo,
		"latency.answer_ms", time.Since(answerStart).Milliseconds())
	return full.String(), nil
}

// Type 返回模型类型标识。
func (m *AutoRouterModel) Type() string { return "auto" }

// Close 释放懒连接的 MCP 客户端；可安全多次调用，未连接时为空操作。
//
// 注意：本方法不在 domain/chat.Model 端口内，由 bootstrap 在关闭阶段按需调用。
func (m *AutoRouterModel) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeMCP()
	// 清空 agent，下次请求会重新 ensureAgent（可再次尝试 MCP）
	m.agent = nil
}
