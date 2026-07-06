# AGENTS.md

## 模块职责

- 本目录承载 Gin HTTP 接口层：控制器、DTO、路由、中间件、SSE 和通用响应工具。

## 变更约束

- 对外响应继续使用统一信封 `{ status_code, status_msg, data }`，不要在局部接口偷偷返回异形结构。
- 新接口优先沿用现有路由分组、`httpx.Handler` 包装和 JWT 鉴权模式。
- 仅在这里处理 HTTP 概念，如 header、query、body、SSE 和状态码；业务流程仍停留在应用层。

## 验证

- HTTP 契约改动后运行 `go test ./test/... -v`，并至少人工验证一个受影响接口的请求/响应。
