package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"GopherAI/internal/domain/chat"
	"GopherAI/pkg/logger"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPModel 集成 MCP 工具调用能力，实现 domain/chat.Model 端口。
type MCPModel struct {
	llm        einomodel.ToolCallingChatModel
	mcpClient  *client.Client
	accountNo  string
	mcpBaseURL string
}

// aiToolCall 表示 AI 输出的工具调用请求。
type aiToolCall struct {
	IsToolCall bool                   `json:"isToolCall"`
	ToolName   string                 `json:"toolName"`
	Args       map[string]interface{} `json:"args"`
}

// NewMCPModel 创建 MCP 模型实例。
func NewMCPModel(ctx context.Context, accountNo, modelName, baseURL, mcpBaseURL string) (*MCPModel, error) {
	key := os.Getenv("OPENAI_API_KEY")
	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		logger.Error("NewMCPModel failed", "accountNo", accountNo, "err", err)
		return nil, fmt.Errorf("create mcp model failed: %v", err)
	}
	logger.Info("NewMCPModel success", "accountNo", accountNo, "baseURL", mcpBaseURL)
	return &MCPModel{llm: llm, mcpBaseURL: mcpBaseURL, accountNo: accountNo}, nil
}

// 编译期断言：MCPModel 必须满足领域模型端口。
var _ chat.Model = (*MCPModel)(nil)

// Generate 生成同步回复，并在需要时执行 MCP 工具调用。
func (m *MCPModel) Generate(ctx context.Context, history []chat.Message) (string, error) {
	messages := toSchemaMessages(history)
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	query := messages[len(messages)-1].Content
	firstMessages := m.buildPromptMessages(messages, m.buildFirstPrompt(query))
	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		logger.Error("MCPModel Generate first failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp first generate failed: %v", err)
	}

	toolCall, err := m.parseAIResponse(firstResp.Content)
	if err != nil || !toolCall.IsToolCall {
		return firstResp.Content, nil
	}

	toolResult, err := m.executeToolCall(ctx, toolCall)
	if err != nil {
		logger.Error("MCPModel Generate executeToolCall failed", "accountNo", m.accountNo, "err", err)
		return firstResp.Content, nil
	}

	secondMessages := m.buildPromptMessages(messages, m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult))
	finalResp, err := m.llm.Generate(ctx, secondMessages)
	if err != nil {
		logger.Error("MCPModel Generate second failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp second generate failed: %v", err)
	}
	return finalResp.Content, nil
}

// Stream 生成流式回复，并在需要时执行 MCP 工具调用。
func (m *MCPModel) Stream(ctx context.Context, history []chat.Message, cb chat.StreamCallback) (string, error) {
	messages := toSchemaMessages(history)
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	query := messages[len(messages)-1].Content
	firstMessages := m.buildPromptMessages(messages, m.buildFirstPrompt(query))
	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		logger.Error("MCPModel Stream first failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp first generate failed: %v", err)
	}

	toolCall, err := m.parseAIResponse(firstResp.Content)
	if err != nil || !toolCall.IsToolCall {
		// 不需要工具时，直接把首轮结果作为完整回复返回（与旧实现一致，不分片）。
		return firstResp.Content, nil
	}

	toolResult, err := m.executeToolCall(ctx, toolCall)
	if err != nil {
		logger.Error("MCPModel Stream executeToolCall failed", "accountNo", m.accountNo, "err", err)
		return firstResp.Content, nil
	}

	secondMessages := m.buildPromptMessages(messages, m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult))
	stream, err := m.llm.Stream(ctx, secondMessages)
	if err != nil {
		logger.Error("MCPModel Stream second failed", "accountNo", m.accountNo, "err", err)
		return "", fmt.Errorf("mcp second stream failed: %v", err)
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
			return "", fmt.Errorf("mcp second stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			full.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return full.String(), nil
}

// executeToolCall 执行工具调用并返回文本结果。
func (m *MCPModel) executeToolCall(ctx context.Context, toolCall *aiToolCall) (string, error) {
	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		return "", err
	}
	return m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
}

// buildPromptMessages 复制原始消息并替换最后一条消息。
func (m *MCPModel) buildPromptMessages(messages []*schema.Message, prompt string) []*schema.Message {
	out := make([]*schema.Message, len(messages))
	copy(out, messages)
	out[len(out)-1] = &schema.Message{Role: schema.User, Content: prompt}
	return out
}

// getMCPClient 获取或创建 MCP 客户端。
func (m *MCPModel) getMCPClient(ctx context.Context) (*client.Client, error) {
	if m.mcpClient == nil {
		httpTransport, err := transport.NewStreamableHTTP(m.mcpBaseURL)
		if err != nil {
			logger.Error("MCPModel transport failed", "baseURL", m.mcpBaseURL, "err", err)
			return nil, fmt.Errorf("create mcp transport failed: %v", err)
		}
		m.mcpClient = client.NewClient(httpTransport)
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{Name: "MCP-Go AIHelper Client", Version: "1.0.0"}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}
		if _, err := m.mcpClient.Initialize(ctx, initRequest); err != nil {
			logger.Error("MCPModel initialize failed", "baseURL", m.mcpBaseURL, "err", err)
			return nil, fmt.Errorf("mcp client initialize failed: %v", err)
		}
	}
	return m.mcpClient, nil
}

// buildFirstPrompt 构建首轮提示词，让模型判断是否需要工具调用。
func (m *MCPModel) buildFirstPrompt(query string) string {
	return fmt.Sprintf(`你是一个智能助手，可以调用MCP工具来获取信息。

可用工具:
- get_weather: 获取指定城市的天气信息，参数: city（城市名称，支持中文和英文，如北京、Shanghai等）

重要规则:
1. 如果需要调用工具，必须严格返回以下JSON格式：
{
  "isToolCall": true,
  "toolName": "工具名称",
  "args": {"参数名": "参数值"}
}
2. 如果不需要调用工具，直接返回自然语言回答
3. 请根据用户问题决定是否需要调用工具

用户问题: %s

请根据需要调用适当的工具，然后给出综合的回答。`, query)
}

// buildSecondPrompt 构建第二轮提示词，回填工具结果。
func (m *MCPModel) buildSecondPrompt(query, toolName string, args map[string]interface{}, toolResult string) string {
	return fmt.Sprintf(`你是一个智能助手，可以调用MCP工具来获取信息。

工具执行结果:
工具名称: %s
工具参数: %v
工具结果: %s

用户问题: %s

请根据工具结果和用户问题，给出最终的综合回答。`, toolName, args, toolResult, query)
}

// parseAIResponse 解析 AI 响应，识别是否包含工具调用。
func (m *MCPModel) parseAIResponse(response string) (*aiToolCall, error) {
	var toolCall aiToolCall
	if err := json.Unmarshal([]byte(response), &toolCall); err == nil {
		return &toolCall, nil
	}
	if strings.Contains(response, "get_weather") {
		if city := m.extractCity(response); city != "" {
			return &aiToolCall{IsToolCall: true, ToolName: "get_weather", Args: map[string]interface{}{"city": city}}, nil
		}
	}
	return &aiToolCall{IsToolCall: false}, nil
}

// callMCPTool 调用 MCP 工具并提取文本结果。
func (m *MCPModel) callMCPTool(ctx context.Context, mcpClient *client.Client, toolName string, args map[string]interface{}) (string, error) {
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: toolName, Arguments: args}}
	result, err := mcpClient.CallTool(ctx, req)
	if err != nil {
		logger.Error("MCPModel callMCPTool failed", "tool", toolName, "err", err)
		return "", fmt.Errorf("mcp tool call failed: %v", err)
	}
	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text += tc.Text + "\n"
		}
	}
	return text, nil
}

// extractCity 从响应中提取城市名称。
func (m *MCPModel) extractCity(response string) string {
	var toolCall aiToolCall
	if err := json.Unmarshal([]byte(response), &toolCall); err == nil {
		if city, ok := toolCall.Args["city"].(string); ok {
			return city
		}
	}
	return ""
}

// Type 返回模型类型标识。
func (m *MCPModel) Type() string { return "3" }

// Close 关闭 MCP 客户端。
func (m *MCPModel) Close() {
	if m.mcpClient != nil {
		m.mcpClient.Close()
	}
}
