# AGENTS.md

## 模块职责

- `App.vue` 只承载路由出口与少量全局外壳，`main.js` 负责应用入口、插件注册和全局样式引入。
- `assets/styles/` 提供 `tokens.css`、`gradients.css`、`chat.css`，分别管理设计变量、渐变背景和聊天共享样式。
- `components/` 存放纯展示组件，包含 `GradientBackground`、`auth/AuthCard` 和 `chat/` 下的会话、顶部条、消息和输入组件。
- `composables/` 存放可复用副作用逻辑，覆盖会话、发送、SSE、RAG 文件、TTS 轮询和自动滚动。
- `layouts/` 存放跨页面骨架，当前核心是 `ChatLayout`；`views/` 只做页面级装配。

## 变更约束

- 保持依赖方向为 `views -> layouts/components/composables -> utils/api`，禁止组件层依赖业务请求、路由守卫或页面状态。
- 新增网络、轮询、计时器、SSE 解析等副作用时优先放入 `composables/`，并通过参数和返回值与视图通信。
- 新增跨页面视觉规则时优先放入 `assets/styles/`，通过 CSS 变量复用，不要在多个页面复制相同渐变、气泡或卡片样式。
- 前端错误处理和关键流程记录优先使用 `utils/frontendLogger.js`，避免散落裸 `console`。
- 新页面或全局状态变化要同步检查登录跳转、鉴权守卫和后端响应信封契约。
