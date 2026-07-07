# AGENTS.md

## 模块职责

- `vue-frontend/` 是 Vue 3 前端应用，负责登录注册、菜单导航、AI 对话和图片识别等用户界面。
- 重构后的前端按变化频率分层：`views/` 做页面编排，`components/` 做纯展示，`composables/` 做可复用副作用，`layouts/` 做跨页面骨架，`assets/styles/` 做视觉 token 与共享样式。

## 架构约束

- 默认保持功能、路由和后端接口契约不变；前端继续通过 `/api` 代理访问后端，鉴权依赖 `localStorage` 中的 `token`。
- 遵循单向依赖：`views -> layouts/components/composables -> utils/api`；组件只接收 `props` 并抛出 `events`，行为逻辑不要反向依赖视图。
- UI 微调优先落到 `assets/styles/`、小组件或布局组件，避免把重复样式和网络 / 轮询 / SSE 等副作用塞回页面级组件。
- 继续复用 Element Plus、现有 `router/` 和 `utils/api.js`，不要为单个页面复制 axios 实例或请求封装层。

## 验证

- 前端改动后在 `vue-frontend/` 目录运行 `npm run lint`。
- 页面交互、路由或鉴权有明显变化时，手动检查受影响主流程和移动端布局。
