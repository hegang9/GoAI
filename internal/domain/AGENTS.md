# AGENTS.md

## 模块职责

- 领域层定义实体、值对象、聚合、领域错误和端口接口，是仓库架构的稳定核心。

## 分层约束

- 禁止引入 `config`、`application`、`infrastructure`、`interfaces` 或具体框架依赖；`test/architecture_test.go` 会校验这条规则。
- 领域端口描述“需要什么能力”，不描述“如何用 Gin/GORM/Redis/MCP 去实现”。

## 变更约束

- 优先用领域语言命名类型和方法，不要把 HTTP、SQL、Redis key、第三方 SDK 词汇泄漏到领域 API。
- 新增端口时先确认是否真是领域需要，而不是某个适配器实现细节。

## 验证

- 领域层改动后务必运行 `go test ./test/... -v`，重点关注 `test/architecture_test.go`。
