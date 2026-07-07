# AGENTS.md

## 模块职责

- `views/` 是页面级编排层，只负责组合布局、展示组件和 composable，不承载可复用底层逻辑。
- `Login.vue` 与 `Register.vue` 复用 `GradientBackground` 和 `AuthCard`；`Menu.vue` 使用数据驱动菜单项。
- `AIChat.vue` 装配 `ChatLayout`、聊天子组件和 `useChatSession`、`useChatSend`、`useRagFiles`、`useTTS`。
- `ImageRecognition.vue` 复用 `ChatLayout`、`MessageList` 和消息气泡样式，避免复制聊天页骨架。

## 变更约束

- 页面文件应保持轻量：业务副作用下沉到 `composables/`，共享 UI 下沉到 `components/` 或 `layouts/`，共享视觉下沉到 `assets/styles/`。
- 不要在页面内复制 axios 实例、路由守卫、登录鉴权、渐变背景、聊天气泡或认证卡片样式。
- 流式对话、文件上传、TTS 轮询和图片识别等长链路交互必须保留明确的加载态、失败提示和错误日志。
- 新页面或接口字段变化要同步确认后端 DTO、错误码和 `{ status_code, status_msg, data }` 响应信封仍匹配。

## 验证

- 页面改动后运行 `npm run lint`，并至少手动检查受影响页面的主流程。
