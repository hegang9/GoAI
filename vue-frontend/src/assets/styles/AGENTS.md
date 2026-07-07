# AGENTS.md

## 模块职责

- `assets/styles/` 是前端共享视觉层，负责把跨页面样式收口到稳定入口。
- `tokens.css` 管理颜色、圆角、阴影、间距和过渡 CSS 变量。
- `gradients.css` 管理统一渐变背景和颗粒动画。
- `chat.css` 管理会话侧栏、顶部条、消息气泡和输入区共享样式。

## 变更约束

- 优先通过 `tokens.css` 调整全局视觉数值，不要在页面或组件里硬编码同类颜色、圆角和阴影。
- 修改 `chat.css` 时要同时考虑 `AIChat.vue` 与 `ImageRecognition.vue` 的复用效果。
- 修改 `gradients.css` 时要保持 `GradientBackground.vue` 的类名契约稳定。
- 不要把页面结构、业务状态、网络逻辑或组件私有行为写入共享样式。

## 验证

- 样式改动后运行 `npm run lint`，并手动检查受影响页面在桌面和窄屏下的主要布局。
