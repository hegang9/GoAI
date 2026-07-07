# AGENTS.md

## 模块职责

- `utils/` 提供前端基础设施工具，当前核心是 `api.js` axios 客户端和 `frontendLogger.js` 前端日志封装。
- 本目录可被 `composables/` 和少量入口代码依赖，但不依赖 `views/`、`components/` 或 `layouts/`。

## 变更约束

- `api.js` 中对 `{ status_code, status_msg, data }` 的展平兼容是前后端契约关键点；不要随意改写返回结构。
- 401 处理仍应清 token 并跳回登录页，避免让未授权状态在页面内静默扩散。
- 新增通用工具时保持无 UI 依赖，避免把页面状态、DOM 操作、组件事件或具体业务流程放进 `utils/`。
- 错误处理、重要控制流和性能敏感路径应通过 `frontendLogger.js` 记录，日志内容要有排查价值且避免噪声。

## 验证

- 修改后运行 `npm run lint`，并手动验证一个登录态请求和一个 401 场景。
