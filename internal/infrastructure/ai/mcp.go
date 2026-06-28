package ai

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"GopherAI/internal/domain/chat"
	"GopherAI/pkg/logger"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	mcpcli "github.com/mark3labs/mcp-go/client"
)

// mcpSystemPrompt 作为 ReAct Agent 的人设/路由说明：由模型自行决定是否调用工具。
// 取代旧实现里把工具名和参数硬编码进提示词、再手工解析 JSON 的脆弱做法。
const mcpSystemPrompt = `你是一个智能助手，可在需要时调用工具获取实时信息（如天气）。
请根据用户问题自行决定是否调用工具；不需要工具时直接用自然语言回答。`

// MCPModel 基于 Eino ReAct Agent + 原生工具调用实现 domain/chat.Model 端口。
//
// 设计要点：
//   - 工具调用由 Eino 原生 schema.ToolCalls 驱动，模型通过 WithTools 感知工具，
//     ReAct Agent 自动完成「模型→（有 ToolCalls 则执行工具→回灌）→模型」的多轮循环；
//   - MCP 客户端采用懒连接：构造模型时不连接 MCP Server，首次 Generate/Stream 时才建立连接、
//     拉取工具并构建 Agent，避免 Server 暂不可用导致模型创建失败。
type MCPModel struct {
	// llm 预创建的对话模型；构造时仅初始化客户端，不产生网络开销。
	llm einomodel.ToolCallingChatModel
	// accountNo 账号编号，仅用于日志定位。
	accountNo string
	// mcpBaseURL MCP Server 地址，懒连接时使用。
	mcpBaseURL string

	// mu 保护懒初始化的并发安全。
	mu sync.Mutex
	// agent 懒构建的 ReAct Agent；为 nil 表示尚未完成初始化。
	agent *react.Agent
	// mcpClient 懒连接的 MCP 客户端，随 agent 一同初始化，供关闭时释放。
	mcpClient *mcpcli.Client
}

// NewMCPModel 创建基于 ReAct 的 MCP 模型实例。
//
// 注意：此处不连接 MCP Server（懒连接），即使 Server 暂不可用也能成功创建模型，
// 真正的连接、工具拉取与 Agent 构建延迟到首次 Generate/Stream 调用。
func NewMCPModel(ctx context.Context, accountNo, modelName, baseURL, apiKey, mcpBaseURL string) (*MCPModel, error) {
	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  apiKey,
	})
	if err != nil {
		logger.Error("NewMCPModel create model failed", "accountNo", accountNo, "err", err)
		return nil, fmt.Errorf("create mcp model failed: %v", err)
	}
	logger.Info("NewMCPModel success", "accountNo", accountNo, "mcpBaseURL", mcpBaseURL)
	return &MCPModel{llm: llm, accountNo: accountNo, mcpBaseURL: mcpBaseURL}, nil
}

// 编译期断言：MCPModel 必须满足领域模型端口。
var _ chat.Model = (*MCPModel)(nil)

// ensureAgent 懒加载 ReAct Agent：首次调用时连接 MCP Server、拉取并转换工具、构建带工具循环的 Agent。
//
// 初始化失败不会缓存错误状态：本次返回错误，下一次调用会重新尝试连接，
// 以提升 MCP Server 临时不可用场景下的健壮性。
func (m *MCPModel) ensureAgent(ctx context.Context) (*react.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 已初始化则直接复用，避免重复连接与重复构建。
	if m.agent != nil {
		return m.agent, nil
	}

	// 懒连接 MCP Server 并完成协议握手。
	mcpClient, err := newMCPClient(ctx, m.mcpBaseURL)
	if err != nil {
		logger.Error("MCPModel ensureAgent connect mcp failed", "accountNo", m.accountNo, "err", err)
		return nil, err
	}

	// 通过 eino-ext 适配器把 MCP Server 上的工具批量转换为 Eino 工具。
	tools, err := newMCPTools(ctx, mcpClient)
	if err != nil {
		mcpClient.Close()
		logger.Error("MCPModel ensureAgent build tools failed", "accountNo", m.accountNo, "err", err)
		return nil, err
	}

	// 用 ReAct Agent 封装「模型↔工具」循环，内部自动 WithTools 并构建 ToolsNode。
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: m.llm,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: tools},
		// 每轮模型调用前注入系统提示词作为人设/路由说明。
		MessageModifier: func(_ context.Context, in []*schema.Message) []*schema.Message {
			return append([]*schema.Message{schema.SystemMessage(mcpSystemPrompt)}, in...)
		},
	})
	if err != nil {
		mcpClient.Close()
		logger.Error("MCPModel ensureAgent create react agent failed", "accountNo", m.accountNo, "err", err)
		return nil, fmt.Errorf("create react agent failed: %v", err)
	}

	m.mcpClient = mcpClient
	m.agent = agent
	logger.Info("MCPModel ensureAgent success", "accountNo", m.accountNo, "mcpBaseURL", m.mcpBaseURL, "toolCount", len(tools))
	return agent, nil
}

// Generate 由 ReAct Agent 自动完成（可能多轮）工具调用并返回最终回复。
func (m *MCPModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	agent, err := m.ensureAgent(ctx)
	if err != nil {
		logger.Error("MCPModel Generate ensureAgent failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp ensure agent failed: %v", err)
	}

	out, err := agent.Generate(ctx, toSchemaMessages(history))
	if err != nil {
		logger.Error("MCPModel Generate failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp generate failed: %v", err)
	}
	return out.Content, nil
}

// Stream 由 ReAct Agent 流式产出最终回复（工具调用阶段不向前端分片，与旧行为一致）。
func (m *MCPModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	agent, err := m.ensureAgent(ctx)
	if err != nil {
		logger.Error("MCPModel Stream ensureAgent failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp ensure agent failed: %v", err)
	}

	stream, err := agent.Stream(ctx, toSchemaMessages(history))
	if err != nil {
		logger.Error("MCPModel Stream failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp stream failed: %v", err)
	}
	defer stream.Close()

	var full strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("MCPModel Stream recv failed", "accountNo", m.accountNo, "err", err)
			return "", fmt.Errorf("mcp stream recv failed: %v", err)
		}
		// 仅转发有内容的最终回复分片；触发工具调用的中间消息内容为空，自然被跳过。
		if len(msg.Content) > 0 {
			full.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return full.String(), nil
}

// Type 返回模型类型标识（保持 "3"，路由不变）。
func (m *MCPModel) Type() string { return "3" }

// Close 释放懒连接的 MCP 客户端；可安全多次调用，未连接时为空操作。
func (m *MCPModel) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mcpClient != nil {
		if err := m.mcpClient.Close(); err != nil {
			logger.Warn("MCPModel Close mcp client failed", "accountNo", m.accountNo, "err", err)
		}
		m.mcpClient = nil
		m.agent = nil
	}
}
