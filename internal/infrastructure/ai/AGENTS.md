# AGENTS.md

## 模块职责

- 本目录实现各类对话模型适配器与模型工厂，统一承接 OpenAI、RAG、MCP、Ollama 等模型类型。

## 变更约束

- 继续维护模型类型约定：`1=OpenAI`、`2=RAG`、`3=MCP`、`4=Ollama`；任何变动都要同步审视历史会话与上层路由。
- `Factory` 只负责按类型创建模型，不要把会话编排、HTTP DTO 或控制器逻辑拉进来。
- RAG 与 MCP 适配器都依赖 `account_no`；新增模型若也需要上下文参数，应沿用显式 `params` 传递，而不是读全局状态。
- MCP 保持懒连接与工具发现机制，RAG 保持检索增强与 query rewrite 的边界清晰，不要把两条链路混成一套临时逻辑。
- `MCPModel` 必须继续懒连接；`NewMCPModel()` 不应在构造阶段要求 MCP 服务可达，否则会破坏启动与回放链路。
- `RAGModel` 在无文档或无相关上下文时必须保留原始消息，不要无条件改写最后一条用户消息。
- 会话缓存键当前是 `accountNo + sessionID` 而不是 `modelType`；改模型编号或默认模型类型时，要同步评估缓存命中和回放语义。

## 验证

- 修改后运行 `go test ./internal/infrastructure/ai -v`，并补充 `go test ./test/... -v`。
- MCP 相关改动至少覆盖 `go test ./internal/infrastructure/ai -run 'TestMCP(ClientAndToolsDiscovery|ToolInvoke)$'`。
