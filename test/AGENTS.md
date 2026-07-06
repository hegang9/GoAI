# AGENTS.md

## 模块职责

- `test/` 负责仓库级回归测试，覆盖架构约束、公共工具、配置解码、会话流程、RAG 链路和存储安全等核心行为。

## 变更约束

- 新测试优先验证稳定的外部行为和架构边界，不要对实现细节做脆弱断言。
- 能并行的测试继续使用 `t.Parallel()`；依赖文件系统、端口或共享状态时先确认隔离策略。
- 架构测试是硬约束：领域层纯净、基础设施层不反向依赖接口/应用层，改架构前先更新测试与设计说明。
- 临时调试数据不要提交进 `test/uploads` 或其他固定目录。
- 把这些测试视为“可执行架构文档”：RAG 的 `chunk_N`、Markdown/中文切分、路径安全、删除语义和分层规则都由这里锁定。

## 验证

- 本目录改动后运行 `go test ./test/... -v`。
- 若同步修改了 `internal/infrastructure/rag` 或 `internal/infrastructure/ai`，再补对应包级测试。
- MCP 相关回归主要在 `internal/infrastructure/ai/mcp_smoke_test.go`，不要误以为顶层 `test/` 已经覆盖了完整 MCP 协议链路。
