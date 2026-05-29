# 日志系统升级方案：`log` → `log/slog`

## Context

项目当前直接使用 Go 标准库 `log` 包裸写日志，无分级、无结构化输出、无调用位置信息。项目运行在 Go 1.24，已内置 `log/slog`（Go 1.21 引入的结构化日志标准库），升级零额外依赖。

## 影响范围评估

### 统计

| 指标 | 数值 |
|------|------|
| 总 log 调用数 | **66 处**（main module） |
| 涉及文件数 | **14 个 .go 文件** |
| `log.Fatal` / `log.Fatalf` | 7 处（需特殊处理） |
| `log.Printf` | 25 处 |
| `log.Println` | 34 处 |
| MCP 模块（独立 module） | 6 处，2 文件（**本次不动**） |

### 涉及文件清单

| 文件 | 调用数 | 主要日志类型 |
|------|--------|-------------|
| `service/session/session.go` | 13 | 错误 + SSE 调试 |
| `common/aihelper/model.go` | 14 | 错误 + RAG + MCP 工具 |
| `service/file/file.go` | 11 | 文件操作各阶段 |
| `main.go` | 5 | 启动信息 |
| `common/tts/tts.go` | 5 | TTS API 调试 |
| `controller/file/file.go` | 3 | 请求错误 |
| `service/image/image.go` | 3 | 图像识别错误 |
| `controller/image/image.go` | 2 | 请求错误 |
| `common/rabbitmq/rabbitmq.go` | 2 | 连接日志 + Fatal |
| `config/config.go` | 1 | Fatal |
| `controller/tts/tts.go` | 1 | 错误 |
| `middleware/jwt/jwt.go` | 1 | 调试日志（token 明文，安全风险） |
| `common/mcp/server/server.go` | 1 | 启动信息 |
| `common/mcp/main.go` | 5 | 独立 module，Fatal ×5 |

## 方案：`log/slog` + 轻量封装

### 为什么选 `log/slog` 而非 `logrus` 或 `zap`

- **零新依赖**：Go 1.24 内置，不增加 go.sum
- `logrus` 已停维（作者声明不再添加新功能）
- `zap` 性能最强但 API 复杂，对当前项目过度
- `slog` 的 `slog.Info("msg", "key", val)` 风格与项目现有的 `log.Println("msg", val)` 迁移成本最低

### 新增文件：`common/slog/slog.go`

封装初始化 + 常用函数，避免每个文件重复配置：

```go
package slog

// 提供 Init() 初始化全局 logger
// 提供 Info(msg, args...)、Error(msg, args...)、Warn(...)、Debug(...)、Fatal(...)
// Debug 级别通过环境变量或配置控制
// 自动附加调用位置（文件名:行号）
```

- `Info`：启动成功、连接就绪、文件上传完成
- `Error`：各类操作失败（替代现有大部分 log.Println）
- `Debug`：SSE chunk 内容、token 值、API 原始响应
- `Warn`：非致命异常（RAG 索引不存在等）
- `Fatal`：初始化阶段不可恢复的错误（替代现有 log.Fatal）

### 改造原则

1. **机器替换**：`log.Println("xxx error:", err)` → `slog.Error("xxx", "err", err)`
2. **分级归类**：调试信息降级为 `Debug`，启动信息保留 `Info`
3. **安全修复**：`middleware/jwt/jwt.go:34` 打印 token 明文，改为 Debug 级别且脱敏（仅打印前 10 字符）
4. **MCP 模块不动**：它是独立 Go module（`github.com/kaitai/gopherai-mcp`），6 处 log 调用，与主模块解耦，本次不改造
5. **Fatal 处理**：`slog` 不支持 `Fatal`（不自带 os.Exit），封装中手动实现

### 迁移对照示例

```go
// Before
log.Println("CreateSessionAndSendMessage CreateSession error:", err)

// After
slog.Error("CreateSessionAndSendMessage CreateSession", "err", err)
```

```go
// Before
log.Printf("[readDataFromDB] failed to create helper for user=%s session=%s: %v", ...)

// After
slog.Error("readDataFromDB failed to create helper", "user", userName, "session", sessionID, "err", err)
```

```go
// Before (main.go:66)
log.Println("redis init success  ")

// After
slog.Info("redis init success")
```

## 实施步骤

### Step 1：创建 `common/slog/slog.go`

- 封装 `slog` 初始化，配置 JSON 格式输出（生产环境）或 Text 格式（开发环境）
- 暴露 `Info/Error/Warn/Debug/Fatal` 五个函数
- 自动附加 `source` 属性（文件名:行号）

### Step 2：逐文件迁移（按影响大小排序）

1. `main.go`（5 处，入口文件先改）
2. `config/config.go`（1 处 Fatal）
3. `common/rabbitmq/rabbitmq.go`（2 处）
4. `service/session/session.go`（13 处，最大文件）
5. `common/aihelper/model.go`（14 处，最多调用）
6. `service/file/file.go`（11 处）
7. `common/tts/tts.go`（5 处）
8. `service/image/image.go`（3 处）
9. `controller/file/file.go`（3 处）
10. `controller/image/image.go`（2 处）
11. `controller/tts/tts.go`（1 处）
12. `middleware/jwt/jwt.go`（1 处 + 安全修复）
13. `common/mcp/server/server.go`（1 处）

### Step 3：验证

- `go build ./...` 编译通过
- `go vet ./...` 无警告
- 检查：所有 `"log"` import 被替换或移除
- 检查：无残留的 `log.Println/log.Printf/log.Fatal`

## 风险评估

| 风险 | 等级 | 缓解 |
|------|------|------|
| 语法错误导致编译失败 | 低 | 逐文件改，每改一个就 build 验证 |
| SSE 调试日志量过大 | 低 | 降级为 Debug，默认不输出 |
| log.Fatal 行为差异 | 低 | 封装中手动 os.Exit(1) |
| MCP 模块遗漏 | 无 | 独立 module，明确不动 |
| 日志格式变化影响运维 | 低 | 开发环境保持易读，生产用 JSON |
