# AGENTS.md

## 模块职责

- 本目录实现各类对话模型适配器与模型工厂，统一承接 auto、Ollama 等模型类型。
- `auto` 是统一自动编排模型：planner 检索决策 + RetrievalModifier 一次性检索增强 + ReAct Agent（默认 MCP 工具集，模型自主 native function calling）。

## 变更约束

- 模型类型约定：`auto` 自动编排（主力，读 `[autoModelConfig]`）、`4` Ollama；旧 `1` OpenAI（`openai.go` 保留但工厂已无 `case "1"` 触发入口）/ `2` RAG / `3` MCP 已退役，由 `auto` 统一承载。任何类型变动都要同步审视历史会话与上层路由。
- `Factory` 只负责按类型创建模型，不要把会话编排、HTTP DTO 或控制器逻辑拉进来。
- `auto` 依赖 `account_no`（检索与工具集都按账号隔离）；新增模型若也需要上下文参数，应沿用显式 `params` 传递，而不是读全局状态。
- 检索是 pre-generation 上下文准备，必须在进入 ReAct 前由 `RetrievalModifier` 一次性完成；不要把检索塞进 ReAct 的 `MessageModifier`——后者每轮模型调用前都会触发，且循环中途最后一条是工具结果，重复检索既昂贵又会改错位置。`MessageModifier` 只承载幂等的系统提示注入。
- 职责去重：`Planner` 已产出 `RetrievalQuery` 与 `DocFilter`，`RetrievalModifier` 不再重复 query 改写与 filter 解析，直接用 planner 输出检索。
- `AutoRouterModel` 的 MCP 客户端必须懒连接：构造阶段不连 MCP Server，首次 `Generate`/`Stream` 才建连、拉工具、构建 Agent；MCP 临时不可用时降级为纯生成，且不缓存无工具 Agent，下次调用重试以自动恢复工具能力。
- `RetrievalModifier` 在无文档、无相关上下文或检索失败时必须透传原始消息，不要无条件改写最后一条用户消息。
- 会话缓存键当前是 `accountNo + sessionID` 而不是 `modelType`；改模型编号或默认模型类型时，要同步评估缓存命中和回放语义。

## 验证

- 修改后运行 `go test ./internal/infrastructure/ai -v`，并补充 `go test ./test/... -v`。
- MCP 相关改动至少覆盖 `go test ./internal/infrastructure/ai -run 'TestMCP(ClientAndToolsDiscovery|ToolInvoke)$'`。
