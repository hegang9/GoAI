# GopherAI API 文档

## 基础信息

- 后端默认地址：`http://localhost:9090`
- API 前缀：`/api/v1`
- 前端开发代理前缀：`/api`，会被代理重写为 `/api/v1`
- 默认请求格式：`application/json`
- 文件上传请求格式：`multipart/form-data`
- 流式对话响应格式：`text/event-stream`

## 通用响应

普通 JSON 接口统一以「信封」结构返回，业务数据放入 `data` 字段：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": { }
}
```

- `status_code`：业务状态码，成功为 `1000`。
- `status_msg`：状态码对应文案。
- `data`：业务数据对象；失败或无业务数据时为 `null`（字段恒存在，无 `omitempty`）。

失败响应示例：

```json
{
  "status_code": 2004,
  "status_msg": "邮箱或密码错误",
  "data": null
}
```

客户端应以 `status_code === 1000` 判断成功，并从 `data` 读取业务数据。

常见业务码：

| 业务码 | 含义 | HTTP 状态 |
| --- | --- | --- |
| `1000` | 成功 | `200` |
| `2001` | 请求参数错误 | `400` |
| `2002` | 账号编号或邮箱已存在 | `409` |
| `2003` | 用户不存在 | `404` |
| `2004` | 邮箱或密码错误 | `400` |
| `2006` | 无效的 Token | `401` |
| `2008` | 验证码错误 | `400` |
| `2009` | 记录不存在 | `404` |
| `4001` | 服务繁忙 | `500` |
| `5001` | 模型不存在 | `404` |
| `5002` | 无法打开模型 | `500` |
| `5003` | 模型运行失败 | `500` |
| `6001` | 语音服务失败 | `500` |

## 认证

除用户注册、登录、验证码接口外，其余接口均需要 JWT。

推荐使用请求头：

```http
Authorization: Bearer <token>
```

JWT 中间件也兼容 URL 参数：

```text
?token=<token>
```

## 模型类型

AI 对话接口通过 `modelType` 选择模型：

| modelType | 模型 |
| --- | --- |
| `1` | OpenAI 兼容普通对话模型，读取 `[aiModelConfig]` 中的 `modelName`、`baseUrl`、`apiKey` |
| `2` | 阿里百炼 RAG 模型，使用上传文档和 Redis Stack 向量索引 |
| `3` | MCP 模型，默认连接 `http://localhost:8081/mcp` |
| `4` | Ollama 模型，代码已预留，当前业务接口未传入 `baseURL` 和 `modelName` |

## 用户接口

### 发送邮箱验证码

```http
POST /api/v1/user/captcha
Content-Type: application/json
```

请求体：

```json
{
  "email": "user@example.com"
}
```

成功响应（验证码接口无业务数据，`data` 为 `null`）：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": null
}
```

说明：验证码存入 Redis，有效期 2 分钟。

### 注册

```http
POST /api/v1/user/register
Content-Type: application/json
```

请求体：

```json
{
  "email": "user@example.com",
  "password": "password",
  "captcha": "123456"
}
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "token": "jwt-token"
  }
}
```

说明：注册成功后系统会生成随机账号编号，并向邮箱发送账号编号信息；用户登录使用邮箱和密码，账号编号仅用于系统内部身份和资源隔离。

### 登录

```http
POST /api/v1/user/login
Content-Type: application/json
```

请求体：

```json
{
  "email": "user@example.com",
  "password": "password"
}
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "token": "jwt-token"
  }
}
```

## AI 对话接口

### 获取会话列表

```http
GET /api/v1/ai/chat/sessions
Authorization: Bearer <token>
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "sessions": [
      {
        "sessionId": "session-id",
        "name": "会话标题"
      }
    ]
  }
}
```

### 创建新会话并发送消息

```http
POST /api/v1/ai/chat/send-new-session
Authorization: Bearer <token>
Content-Type: application/json
```

请求体：

```json
{
  "question": "你好",
  "modelType": "1"
}
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "Information": "AI 回复内容",
    "sessionId": "session-id"
  }
}
```

### 向已有会话发送消息

```http
POST /api/v1/ai/chat/send
Authorization: Bearer <token>
Content-Type: application/json
```

请求体：

```json
{
  "question": "继续回答",
  "modelType": "1",
  "sessionId": "session-id"
}
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "Information": "AI 回复内容"
  }
}
```

### 获取会话历史

```http
POST /api/v1/ai/chat/history
Authorization: Bearer <token>
Content-Type: application/json
```

请求体：

```json
{
  "sessionId": "session-id"
}
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "history": [
      {
        "is_user": true,
        "content": "用户消息"
      },
      {
        "is_user": false,
        "content": "AI 回复"
      }
    ]
  }
}
```

### 创建新会话并流式发送消息

```http
POST /api/v1/ai/chat/send-stream-new-session
Authorization: Bearer <token>
Content-Type: application/json
Accept: text/event-stream
```

请求体：

```json
{
  "question": "请流式回答",
  "modelType": "1"
}
```

响应示例：

```text
data: {"sessionId": "session-id"}

data: 第一段内容

data: 第二段内容

data: [DONE]
```

### 向已有会话流式发送消息

```http
POST /api/v1/ai/chat/send-stream
Authorization: Bearer <token>
Content-Type: application/json
Accept: text/event-stream
```

请求体：

```json
{
  "question": "继续流式回答",
  "modelType": "1",
  "sessionId": "session-id"
}
```

响应示例：

```text
data: 第一段内容

data: 第二段内容

data: [DONE]
```

## TTS 接口

### 创建语音合成任务

```http
POST /api/v1/ai/tts
Authorization: Bearer <token>
Content-Type: application/json
```

请求体：

```json
{
  "text": "需要合成的文本"
}
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "task_id": "tts-task-id"
  }
}
```

### 查询语音合成任务

```http
GET /api/v1/ai/tts/query?task_id=tts-task-id
Authorization: Bearer <token>
```

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "task_id": "tts-task-id",
    "task_status": "Success",
    "task_result": "https://speech-url.example.com/audio.mp3"
  }
}
```

说明：`task_result` 为百度 TTS 返回的音频 URL；任务未完成时可能为空。

## 文件与 RAG 接口

### 上传 RAG 文档

```http
POST /api/v1/file/upload
Authorization: Bearer <token>
Content-Type: multipart/form-data
```

表单字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `file` | file | 是 | 支持 `.md` / `.txt` / `.pdf` / `.docx` |

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "file_path": "uploads/account_no/file-id.md"
  }
}
```

说明：上传后会写入本地 `uploads/<account_no>/`，按 `chunkSize`/`chunkOverlap` 分块后基于 Redis Stack 创建向量索引。多文档知识库语义：上传为追加，不再清理已有文档（索引按账号聚合 `rag_docs:{accountNo}:idx`）。

### 列出 RAG 文档

```http
GET /api/v1/file/list
Authorization: Bearer <token>
```

成功响应（`files` 为当前账号已上传文档的存储文件名列表，可直接用于删除接口）：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "files": ["file-id-1.md", "file-id-2.pdf"]
  }
}
```

### 删除 RAG 文档

```http
POST /api/v1/file/delete
Authorization: Bearer <token>
Content-Type: application/json
```

请求体：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `filenames` | string[] | 是 | 待删除文档的存储文件名（上传响应 `file_path` 的 basename），单次最多 50 个 |

```json
{
  "filenames": ["file-id-1.md", "file-id-2.pdf"]
}
```

成功响应（`deleted` 为实际成功删除的文件名列表）：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "deleted": ["file-id-1.md", "file-id-2.pdf"]
  }
}
```

说明：会同时删除文件与其向量数据，采用尽力而为策略——部分失败仍返回成功并在 `deleted` 中反映实际结果；全部失败返回 `4001`。文件名会按 basename 归一化并校验，防止路径逃逸。

## 图片接口

### 图片识别

```http
POST /api/v1/image/recognize
Authorization: Bearer <token>
Content-Type: multipart/form-data
```

表单字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `image` | file | 是 | 待识别图片 |

成功响应：

```json
{
  "status_code": 1000,
  "status_msg": "success",
  "data": {
    "class_name": "tabby cat"
  }
}
```

说明：当前服务端代码固定读取 `/root/models/mobilenetv2/mobilenetv2-7.onnx` 和 `/root/imagenet_classes.txt`。

## curl 示例

登录并保存 token：

```bash
TOKEN=$(curl -s http://localhost:9090/api/v1/user/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"password"}' \
  | jq -r '.token')
```

发送普通对话：

```bash
curl http://localhost:9090/api/v1/ai/chat/send-new-session \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"question":"你好","modelType":"1"}'
```

上传 RAG 文档：

```bash
curl http://localhost:9090/api/v1/file/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F 'file=@example.md'
```

流式对话：

```bash
curl -N http://localhost:9090/api/v1/ai/chat/send-stream-new-session \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"question":"用三句话介绍 Go","modelType":"1"}'
```
