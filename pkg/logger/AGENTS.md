# AGENTS.md

## 模块职责

- 本目录封装项目统一日志初始化与输出接口。
- 日志级别由 `defaultLogLevel` 常量控制。
- 输出为 stdout + `logs/` 文件双写：debug 模式控制台带 ANSI 颜色级别标签，文件为无色同格式；release 为 JSON。
- `source` 通过 `runtime.Callers` 跳过本包包装层，指向业务调用方（`父目录/文件:行号`）。

## 变更约束

- 保持后端统一日志入口，避免各模块直接各自初始化不同 logger。
- 变更日志格式、级别或字段时，先评估启动日志、错误排障与现有调用点是否受影响。
- 控制台着色不得写入日志文件（继续用独立 Handler 扇出，禁止对共享 MultiWriter 直接塞 ANSI）。

## 验证

- 修改后运行 `go test ./test/... -v`，重点关注 `test/logger_test.go`。
