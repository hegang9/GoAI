# AGENTS.md

## 模块职责

- `composables/` 是行为层，封装可复用副作用和状态协作逻辑。
- 当前职责包括会话加载与切换、普通 / 流式消息发送、SSE 解析、RAG 文件列表与上传、TTS 任务轮询、自动滚动。

## 变更约束

- composable 可以依赖 `vue`、`element-plus`、`utils/api.js` 和必要的浏览器 API，但不要依赖 `views/`、`components/` 或 `layouts/`。
- 新增网络请求、ReadableStream、轮询、定时器、DOM 滚动等副作用时优先放在这里，并通过参数、返回值和回调与页面通信。
- 保持后端接口契约稳定，尤其是 `{ status_code, status_msg, data }` 信封、`sessionId` 首帧、`[DONE]` 和错误回滚语义。
- 需要错误提示时可使用 Element Plus；需要排查日志时优先接入 `utils/frontendLogger.js`，不要吞掉异常。
- 不要在 composable 内直接负责页面布局或渲染组件，状态形状变化要同步检查所有调用视图。

## 验证

- 修改后运行 `npm run lint`，并手动检查受影响链路，如流式发送、TTS、RAG 上传或自动滚动。
