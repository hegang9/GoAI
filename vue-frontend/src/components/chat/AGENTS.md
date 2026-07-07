# AGENTS.md

## 模块职责

- `components/chat/` 存放聊天类纯展示组件，包括会话侧栏、顶部工具条、消息列表、消息气泡和输入区。
- 这些组件服务于 `AIChat.vue` 与 `ImageRecognition.vue`，应保持可复用、可组合。

## 变更约束

- 通过 `props` 接收会话、消息、加载态和开关状态，通过 `emits` 暴露发送、切换、TTS 等交互意图。
- 不要在聊天组件中直接调用 `/api`、解析 SSE、轮询 TTS、上传 RAG 文件或写入路由鉴权逻辑。
- 消息气泡渲染可保留轻量 markdown / 图片 / TTS 展示逻辑；协议解析和状态同步应留在 `composables/` 或页面编排层。
- 聊天共享外观优先修改 `assets/styles/chat.css`，组件内 scoped 样式只处理组件特有且不可共享的细节。
- 修改 `MessageList` 自动滚动或 `ChatInput` 焦点行为时，确保 `AIChat.vue` 的发送体验不回退。

## 验证

- 修改后运行 `npm run lint`，并手动检查 AI 对话和图片识别页的消息展示、输入和窄屏布局。
