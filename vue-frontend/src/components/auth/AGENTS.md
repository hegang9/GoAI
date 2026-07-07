# AGENTS.md

## 模块职责

- `components/auth/` 存放登录与注册共享的认证展示组件。
- `AuthCard.vue` 是认证卡片外壳，负责标题、描述、动画、表单容器和底部链接区域的统一展示。

## 变更约束

- 认证组件只提供外壳和 slot，不直接处理登录、注册请求、token 存储或路由跳转。
- 登录和注册的表单字段、校验、提交行为应继续留在对应 `views/Login.vue` 与 `views/Register.vue` 编排。
- 卡片样式需要复用 `assets/styles/tokens.css` 的视觉变量，避免 `Login.vue` 与 `Register.vue` 再次复制认证卡片样式。
- 修改 slot 结构时同步检查登录页和注册页，确保表单、底部链接和移动端布局仍正常。

## 验证

- 修改后运行 `npm run lint`，并手动检查 `/login` 与 `/register`。
