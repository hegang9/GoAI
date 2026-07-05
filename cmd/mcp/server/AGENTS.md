# AGENTS.md

## 模块职责

- 本目录实现 MCP 服务端、天气工具注册以及对第三方天气 API 的适配。
- 当前稳定对外契约是 Streamable HTTP 的 `/mcp` 端点和工具 `get_weather(city)`。

## 变更约束

- 工具名、参数名、返回文本格式保持稳定，避免悄悄破坏客户端和 `internal/infrastructure/ai` 的工具发现逻辑。
- 外部 API 响应要先转换为项目内部结构，再生成 MCP 返回内容；不要把第三方 JSON 直接透传给上层。
- 参数校验和错误信息要明确，空城市、无天气数据、解析失败等路径不能静默成功。
- 若新增工具，优先继续在 `NewMCPServer()` 内集中注册，让 `StartServer()` 保持纯启动职责。

## 验证

- 在 `cmd/mcp/` 目录下运行 `go test ./...`。
- 协议或工具改动后，至少补一次本地启动服务并从客户端调用 `get_weather` 的联调验证。
