# AGENTS.md

## 模块职责

- DTO 目录定义 HTTP 请求和响应结构，以及统一响应信封。

## 变更约束

- DTO 只描述传输契约，不承载业务方法、数据库 tag 或应用服务依赖。
- 字段名、JSON 结构和可空语义变更前，要同步检查前端读取逻辑与已有接口兼容性。
- 共用响应结构优先复用 `common.go` 中的统一信封，避免复制一套近似类型。
- `file_path`、`filenames`、`files`、`sessionId`、`question`、`modelType` 都是现有前后端契约关键字段；改名应视为 API 变更。
