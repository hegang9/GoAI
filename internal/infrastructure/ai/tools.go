package ai

import (
	"context"
	"fmt"

	"GopherAI/pkg/logger"

	mcptool "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	mcpcli "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// mcpClientInfo 用于在与 MCP Server 握手时声明本客户端身份。
var mcpClientInfo = mcp.Implementation{Name: "GopherAI MCP Client", Version: "1.0.0"}

// newMCPClient 创建并初始化 MCP 客户端：建立 Streamable HTTP 传输并完成协议握手。
//
// 该函数会真正发起到 MCP Server 的网络连接（Initialize 握手），
// 因此应在“懒连接”时机调用，而非模型构造阶段，以避免 Server 暂不可用导致模型创建失败。
func newMCPClient(ctx context.Context, baseURL string) (*mcpcli.Client, error) {
	// 先创建 Streamable HTTP 传输层。
	httpTransport, err := transport.NewStreamableHTTP(baseURL)
	if err != nil {
		logger.Error("newMCPClient create transport failed", "baseURL", baseURL, "err", err)
		return nil, fmt.Errorf("create mcp transport failed: %v", err)
	}

	// 再创建客户端并与 Server 完成协议握手。
	c := mcpcli.NewClient(httpTransport)
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpClientInfo
	initReq.Params.Capabilities = mcp.ClientCapabilities{}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		logger.Error("newMCPClient initialize failed", "baseURL", baseURL, "err", err)
		return nil, fmt.Errorf("mcp client initialize failed: %v", err)
	}

	logger.Info("newMCPClient success", "baseURL", baseURL)
	return c, nil
}

// newMCPTools 通过 eino-ext 的 MCP 适配器，把 MCP Server 暴露的全部工具批量转换为 Eino 工具。
//
// 适配器内部会调用 MCP Server 的 ListTools，并依据每个工具的 InputSchema 自动生成
// Eino 所需的参数 JSON Schema，从而无需为每个工具手写封装，新增工具时本层零改动。
func newMCPTools(ctx context.Context, mcpClient *mcpcli.Client) ([]tool.BaseTool, error) {
	tools, err := mcptool.GetTools(ctx, &mcptool.Config{Cli: mcpClient})
	if err != nil {
		logger.Error("newMCPTools get tools failed", "err", err)
		return nil, fmt.Errorf("get mcp tools failed: %v", err)
	}
	if len(tools) == 0 {
		// 没有可用工具时给出告警：ReAct Agent 仍可工作，但会退化为纯对话。
		logger.Warn("newMCPTools got empty tool list")
	}
	logger.Info("newMCPTools success", "toolCount", len(tools))
	return tools, nil
}
