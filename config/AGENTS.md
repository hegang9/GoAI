# AGENTS.md

## 模块职责

- 本目录定义全局配置结构、默认值策略和 `config/config.toml` 的解码规则。
- 配置是后端启动的唯一事实来源；运行时代码通过 `config.GetConfig()` 读取。

## 变更约束

- 新配置项必须同时更新 `config.go`、`config.toml` 示例、`README.md` 和相关测试。
- 保持字段名、`toml` tag、默认值语义一致；不要引入隐式环境变量兜底，除非明确设计要这样做。
- 配置结构应继续按业务分段组织，如 `mainConfig`、`ragModelConfig`、`chatReplayConfig`，避免把无关项混到一起。
- 与 RAG、MCP、回放等运行期行为强相关的配置改动，要同步审视 `bootstrap` 与对应适配器是否需要更新。

## 验证

- 配置结构改动后运行 `go test ./test/... -v`，重点关注 `test/config_test.go` 与受影响模块的启动路径。
