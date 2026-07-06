# AGENTS.md

## 模块职责

- 本目录编排会话创建、单轮/流式问答、历史查询和冷会话懒加载。
- 它依赖 `domain/chat.Manager` 与仓储端口，不直接感知模型 SDK、MQ 或数据库实现。

## 变更约束

- 保持 `account_no` 作为创建 RAG/MCP 模型时的关键参数，不要打破 `modelParams()` 约定。
- `ensureSessionLoaded` 相关语义必须稳定：冷会话先回放历史，再进入生成或查历史逻辑。
- 新增接口能力时优先复用现有 `AIResult` / `MessageView` 等应用层结构，避免把控制器 DTO 拉进本层。
- 注意会话缓存是按 `accountNo + sessionID` 复用的，不是按 `modelType`；任何模型编号或默认模型类型调整都要评估回放和懒加载路径。

## 验证

- 会话逻辑改动后运行 `go test ./test/... -v`，重点关注 `test/conversation_test.go`。
