# AGENTS.md

## 模块职责

- `layouts/` 存放跨页面共享骨架，当前核心是 `ChatLayout.vue`。
- `ChatLayout` 负责聊天类页面的左侧导航与右侧主区 slot 编排，不负责具体消息、会话或上传业务。

## 变更约束

- 布局组件应只表达结构和 slot 契约，不直接调用接口、解析消息、管理会话或处理鉴权。
- 修改 slot 名称、布局区域或响应式断点时，同步检查 `AIChat.vue` 与 `ImageRecognition.vue` 的装配。
- 通用视觉值引用 `assets/styles/tokens.css`，聊天区域共享样式优先放入 `assets/styles/chat.css`。
- 新增布局前先确认是否真有跨页面复用价值；单页面结构优先留在对应 `views/`。

## 验证

- 修改后运行 `npm run lint`，并手动检查复用该布局的所有页面。
