package ai

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// startInProcessMCPServer 启动一个进程内的 MCP 天气 Server（Streamable HTTP），
// 注册一个固定返回的 get_weather 工具，返回其 /mcp 访问地址与清理函数。
// 仅供冒烟测试使用，避免依赖外部网络与真实 MCP 服务进程。
func startInProcessMCPServer(t *testing.T) (baseURL string, cleanup func()) {
	t.Helper()

	mcpServer := server.NewMCPServer(
		"weather-query-server-test",
		"1.0.0",
		server.WithToolCapabilities(true),
	)
	mcpServer.AddTool(
		mcp.NewTool(
			"get_weather",
			mcp.WithDescription("获取指定城市的天气信息"),
			mcp.WithString("city", mcp.Description("城市名称"), mcp.Required()),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			city, _ := args["city"].(string)
			return mcp.NewToolResultText("城市: " + city + "\n温度: 25.0°C\n天气: 晴"), nil
		},
	)

	streamable := server.NewStreamableHTTPServer(mcpServer) // 默认 endpoint 路径为 /mcp
	ts := httptest.NewServer(streamable)
	return ts.URL + "/mcp", ts.Close
}

// TestMCPClientAndToolsDiscovery 验证「懒连接 + 工具自动发现」核心链路：
// newMCPClient 建链握手 → newMCPTools 经 eino-ext 适配器拉取并转换工具 → 工具 Info 正确。
func TestMCPClientAndToolsDiscovery(t *testing.T) {
	baseURL, cleanup := startInProcessMCPServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := newMCPClient(ctx, baseURL)
	if err != nil {
		t.Fatalf("newMCPClient failed: %v", err)
	}
	defer client.Close()

	tools, err := newMCPTools(ctx, client)
	if err != nil {
		t.Fatalf("newMCPTools failed: %v", err)
	}
	if len(tools) == 0 {
		t.Fatalf("expected at least one tool, got 0")
	}

	// 校验发现到的工具里包含 get_weather。
	var found bool
	for _, tl := range tools {
		info, err := tl.Info(ctx)
		if err != nil {
			t.Fatalf("tool.Info failed: %v", err)
		}
		if info.Name == "get_weather" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected to discover tool 'get_weather'")
	}
}

// TestMCPToolInvoke 验证转换后的 Eino 工具可真正调用 MCP Server 并拿到结果，
// 即 InvokableRun(argumentsInJSON) → mcp tools/call → 结果回传（包含天气文本）。
func TestMCPToolInvoke(t *testing.T) {
	baseURL, cleanup := startInProcessMCPServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := newMCPClient(ctx, baseURL)
	if err != nil {
		t.Fatalf("newMCPClient failed: %v", err)
	}
	defer client.Close()

	tools, err := newMCPTools(ctx, client)
	if err != nil {
		t.Fatalf("newMCPTools failed: %v", err)
	}

	// 找到 get_weather 工具并以 JSON 入参实际调用一次。
	var weather tool.InvokableTool
	for _, tl := range tools {
		info, _ := tl.Info(ctx)
		if info.Name == "get_weather" {
			invokable, ok := tl.(tool.InvokableTool)
			if !ok {
				t.Fatalf("get_weather is not an InvokableTool")
			}
			weather = invokable
		}
	}
	if weather == nil {
		t.Fatalf("get_weather tool not found")
	}

	out, err := weather.InvokableRun(ctx, `{"city":"北京"}`)
	if err != nil {
		t.Fatalf("InvokableRun failed: %v", err)
	}
	// 适配器返回的是整个 CallToolResult 的 JSON，结果文本应包含工具返回内容。
	if !strings.Contains(out, "晴") || !strings.Contains(out, "北京") {
		t.Fatalf("unexpected tool output: %s", out)
	}
}
