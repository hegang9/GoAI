# AGENTS.md

## 模块职责

- 本目录封装项目统一日志初始化与输出接口。
- 日志级别由 `defaultLogLevel` 常量控制；输出为 stdout + `logs/` 文件双写，格式随 `gin.Mode()` 在 Text/JSON 间切换。

## 变更约束

- 保持后端统一日志入口，避免各模块直接各自初始化不同 logger。
- 变更日志格式、级别或字段时，先评估启动日志、错误排障与现有调用点是否受影响。

## 验证

- 修改后运行 `go test ./test/... -v`，重点关注 `test/logger_test.go`。
