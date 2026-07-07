# AGENTS.md

## 模块职责

- `public/` 存放前端静态外壳资源，如 HTML 模板、图标和无需打包处理的静态文件。
- 应用入口、路由、样式和业务逻辑由 `src/main.js` 及其依赖负责，静态外壳只提供挂载点和必要元信息。

## 变更约束

- 保持静态入口轻量，避免把应用逻辑、大量内联样式、渐变背景、聊天样式或认证卡片样式塞回 `public/index.html`。
- 需要调整全局视觉时优先修改 `src/assets/styles/`；需要调整页面结构时优先修改 `src/views/`、`src/layouts/` 或 `src/components/`。
- 修改标题、meta、静态资源路径或挂载节点时，同步确认 `src/main.js` 仍能正常挂载应用。
