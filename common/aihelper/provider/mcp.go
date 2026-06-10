package provider

import (
	"GopherAI/common/logger"
	"GopherAI/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	openaiext "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPModel 表示集成 MCP 工具调用能力的模型实现。
type MCPModel struct {
	// llm 持有底层聊天模型实例。
	llm einomodel.ToolCallingChatModel
	// mcpClient 缓存 MCP 客户端，避免重复初始化。
	mcpClient *client.Client
	// username 标识当前用户，用于后续扩展个性化上下文。
	username string
	// mcpBaseURL 表示 MCP 服务地址。
	mcpBaseURL string
}

// AIToolCall 表示 AI 输出的工具调用请求。
type AIToolCall struct {
	IsToolCall bool                   `json:"isToolCall"`
	ToolName   string                 `json:"toolName"`
	Args       map[string]interface{} `json:"args"`
}

// NewMCPModel 创建 MCP 模型实例。
func NewMCPModel(ctx context.Context, username string) (*MCPModel, error) {
	key := os.Getenv("OPENAI_API_KEY")
	conf := config.GetConfig()
	modelName := conf.RagModelConfig.RagChatModelName
	baseURL := conf.RagModelConfig.RagBaseUrl

	llm, err := openaiext.NewChatModel(ctx, &openaiext.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		logger.Error("NewMCPModel failed", "user", username, "err", err)
		return nil, fmt.Errorf("create mcp model failed: %v", err)
	}

	// mcpBaseURL 当前仍沿用原有本地默认地址，后续可继续收敛到配置层。
	mcpBaseURL := "http://localhost:8081/mcp"
	logger.Info("NewMCPModel success", "user", username, "baseURL", mcpBaseURL)
	return &MCPModel{
		llm:        llm,
		mcpBaseURL: mcpBaseURL,
		username:   username,
	}, nil
}

// GenerateResponse 生成同步响应，并在需要时执行 MCP 工具调用。
func (m *MCPModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content
	firstMessages := m.buildPromptMessages(messages, m.buildFirstPrompt(query))
	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		logger.Error("MCPModel GenerateResponse first generate failed", "user", m.username, "err", err)
		return nil, fmt.Errorf("mcp first generate failed: %v", err)
	}
	logger.Debug("MCP first response", "content", firstResp.Content)

	toolCall, err := m.parseAIResponse(firstResp.Content)
	if err != nil {
		logger.Warn("MCPModel GenerateResponse parse failed", "user", m.username, "err", err)
		return firstResp, nil
	}
	if !toolCall.IsToolCall {
		logger.Debug("MCPModel GenerateResponse no tool call", "user", m.username)
		return firstResp, nil
	}

	toolResult, err := m.executeToolCall(ctx, toolCall)
	if err != nil {
		logger.Error("MCPModel GenerateResponse executeToolCall failed", "user", m.username, "err", err)
		return firstResp, nil
	}

	secondMessages := m.buildPromptMessages(messages, m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult))
	finalResp, err := m.llm.Generate(ctx, secondMessages)
	if err != nil {
		logger.Error("MCPModel GenerateResponse second generate failed", "user", m.username, "err", err)
		return nil, fmt.Errorf("mcp second generate failed: %v", err)
	}
	logger.Debug("MCP final response", "content", finalResp.Content)
	return finalResp, nil
}

// StreamResponse 生成流式响应，并在需要时执行 MCP 工具调用。
func (m *MCPModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content
	firstMessages := m.buildPromptMessages(messages, m.buildFirstPrompt(query))
	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		logger.Error("MCPModel StreamResponse first generate failed", "user", m.username, "err", err)
		return "", fmt.Errorf("mcp first generate failed: %v", err)
	}

	toolCall, err := m.parseAIResponse(firstResp.Content)
	if err != nil {
		logger.Warn("MCPModel StreamResponse parse failed", "user", m.username, "err", err)
		return firstResp.Content, nil
	}
	if !toolCall.IsToolCall {
		return firstResp.Content, nil
	}

	toolResult, err := m.executeToolCall(ctx, toolCall)
	if err != nil {
		logger.Error("MCPModel StreamResponse executeToolCall failed", "user", m.username, "err", err)
		return firstResp.Content, nil
	}

	secondMessages := m.buildPromptMessages(messages, m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult))
	stream, err := m.llm.Stream(ctx, secondMessages)
	if err != nil {
		logger.Error("MCPModel StreamResponse second stream failed", "user", m.username, "err", err)
		return "", fmt.Errorf("mcp second stream failed: %v", err)
	}
	defer stream.Close()

	var finalResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("MCPModel StreamResponse recv failed", "user", m.username, "err", err)
			return "", fmt.Errorf("mcp second stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			finalResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}
	return finalResp.String(), nil
}

// executeToolCall 执行工具调用并返回文本结果。
func (m *MCPModel) executeToolCall(ctx context.Context, toolCall *AIToolCall) (string, error) {
	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		return "", err
	}
	return m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
}

// buildPromptMessages 复制原始消息并替换最后一条消息。
func (m *MCPModel) buildPromptMessages(messages []*schema.Message, prompt string) []*schema.Message {
	promptMessages := make([]*schema.Message, len(messages))
	copy(promptMessages, messages)
	promptMessages[len(promptMessages)-1] = &schema.Message{Role: schema.User, Content: prompt}
	return promptMessages
}

// getMCPClient 获取或创建 MCP 客户端。
func (m *MCPModel) getMCPClient(ctx context.Context) (*client.Client, error) {
	if m.mcpClient == nil {
		httpTransport, err := transport.NewStreamableHTTP(m.mcpBaseURL)
		if err != nil {
			logger.Error("MCPModel getMCPClient transport failed", "baseURL", m.mcpBaseURL, "err", err)
			return nil, fmt.Errorf("create mcp transport failed: %v", err)
		}

		m.mcpClient = client.NewClient(httpTransport)
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "MCP-Go AIHelper Client",
			Version: "1.0.0",
		}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}

		if _, err := m.mcpClient.Initialize(ctx, initRequest); err != nil {
			logger.Error("MCPModel getMCPClient initialize failed", "baseURL", m.mcpBaseURL, "err", err)
			return nil, fmt.Errorf("mcp client initialize failed: %v", err)
		}
	}
	return m.mcpClient, nil
}

// buildFirstPrompt 构建第一次调用的提示词。
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

// buildSecondPrompt 构建第二次调用的提示词。
func (m *MCPModel) buildSecondPrompt(query, toolName string, args map[string]interface{}, toolResult string) string {
	return fmt.Sprintf(`你是一个智能助手，可以调用MCP工具来获取信息。

工具执行结果:
工具名称: %s
工具参数: %v
工具结果: %s

用户问题: %s

请根据工具结果和用户问题，给出最终的综合回答。`, toolName, args, toolResult, query)
}

// parseAIResponse 解析 AI 响应，检查是否包含工具调用。
func (m *MCPModel) parseAIResponse(response string) (*AIToolCall, error) {
	var toolCall AIToolCall
	if err := json.Unmarshal([]byte(response), &toolCall); err == nil {
		return &toolCall, nil
	}
	if strings.Contains(response, "get_weather") {
		city := m.extractCityFromResponse(response)
		if city != "" {
			return &AIToolCall{IsToolCall: true, ToolName: "get_weather", Args: map[string]interface{}{"city": city}}, nil
		}
	}
	return &AIToolCall{IsToolCall: false}, nil
}

// callMCPTool 调用 MCP 工具。
func (m *MCPModel) callMCPTool(ctx context.Context, mcpClient *client.Client, toolName string, args map[string]interface{}) (string, error) {
	callToolRequest := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: toolName, Arguments: args}}
	result, err := mcpClient.CallTool(ctx, callToolRequest)
	if err != nil {
		logger.Error("MCPModel callMCPTool failed", "tool", toolName, "err", err)
		return "", fmt.Errorf("mcp tool call failed: %v", err)
	}

	var text string
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			text += textContent.Text + "\n"
		}
	}
	return text, nil
}

// extractCityFromResponse 从响应中提取城市名称。
func (m *MCPModel) extractCityFromResponse(response string) string {
	var toolCall AIToolCall
	if err := json.Unmarshal([]byte(response), &toolCall); err == nil {
		if args, ok := toolCall.Args["city"].(string); ok {
			return args
		}
	}
	return ""
}

// GetModelType 返回模型类型标识。
func (m *MCPModel) GetModelType() string { return "3" }

// Close 关闭 MCP 客户端。
func (m *MCPModel) Close() {
	if m.mcpClient != nil {
		m.mcpClient.Close()
	}
}
