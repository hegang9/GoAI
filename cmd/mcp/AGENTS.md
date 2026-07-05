# AGENTS.md

## 模块职责

- `cmd/mcp` 是独立 Go module，封装一个可独立启动的 MCP 天气服务与示例客户端。
- 本目录负责模块边界、命令行参数和运行模式选择；协议细节分别落在 `client/` 与 `server/`。
- 这里是本地调试/演示入口，不是生产聊天链路的直接装配点；生产 MCP 走 `internal/infrastructure/ai` 通过 HTTP 访问它。

## 变更约束

- 保持与根模块松耦合，不要依赖根模块的未导出实现、相对路径技巧或运行时单例。
- `server` / `client` 模式的 CLI 语义要稳定，参数名和默认值变更必须同步更新文档与帮助信息。
- 任何协议字段、工具名、HTTP 路径改动都要先检查 `internal/infrastructure/ai` 中的 MCP 集成是否受影响。
- 默认地址和端口约定若变化，要同步检查 `internal/bootstrap/app.go` 里的 `mcpBaseURL` 以及本地调试配置。

## 验证

- 在 `cmd/mcp/` 目录下运行 `go test ./...`。
- 若改了 CLI 或服务地址，再补一次 `go run . --mode server` / `go run . --mode client` 的人工冒烟验证。
