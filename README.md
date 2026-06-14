# GopherAI

GopherAI 是一个 Go + Vue 的 AI 应用示例，后端基于 Gin，前端基于 Vue。当前功能包括用户注册登录、AI 对话、流式对话、RAG 文档上传、图片识别和百度 TTS 语音合成。

## 项目结构

```text
.
├── main.go                 # 后端启动入口
├── config/config.toml      # 后端配置文件
├── router/                 # API 路由
├── controller/             # HTTP 控制器
├── service/                # 业务逻辑
├── common/                 # MySQL/Redis/RabbitMQ/AI/TTS 等公共组件
├── model/ dao/ dto/ bo/    # 数据模型、DAO、传输对象
├── auth/                   # 密码哈希与 JWT 认证
├── random/                 # 随机码生成
├── id/                     # 标识生成
├── fileutil/               # 文件校验与目录清理
├── mapper/                 # model 与 schema 之间的消息转换
├── utils/                  # 遗留通用工具（当前仅保留 MD5）
├── common/aihelper/        # AI 会话管理、模型 provider、工厂与兼容入口
├── vue-frontend/           # Vue 前端
├── docker-compose.yml      # 本地开发中间件编排
├── docs/API.md             # 接口文档
├── docs/gorm_mapping_rules.md # GORM 映射规则说明
├── docs/GORM 常用查询语法速查.md # GORM 常用查询速查
└── 阅读顺序.md              # 项目阅读任务清单
```

## 环境要求

- Go：见 `.go-version` / `.tool-versions`
- Node.js 与 npm：用于运行 `vue-frontend`
- MySQL 8.x：用于用户、会话和消息持久化
- Redis Stack：普通验证码缓存 + RAG RediSearch 向量索引，标准 Redis 不包含 RediSearch
- RabbitMQ：用于异步消息队列，默认队列名 `Message`
- 可选外部服务：OpenAI 兼容模型、阿里百炼 Embedding/Chat、百度 TTS、ONNX 图片识别模型

## 配置说明

后端启动时读取 `config/config.toml`。关键配置如下：

| 配置段 | 作用 |
| --- | --- |
| `[mainConfig]` | 后端监听地址与端口，默认 `0.0.0.0:9090` |
| `[mysqlConfig]` | MySQL 地址、账号、密码、数据库名和字符集 |
| `[redisConfig]` | Redis 地址、密码和 DB |
| `[rabbitmqConfig]` | RabbitMQ 地址、账号、密码和 vhost |
| `[emailConfig]` | 注册验证码邮件配置 |
| `[jwtConfig]` | JWT 过期时间、签发信息和密钥 |
| `[ragModelConfig]` | RAG 使用的模型名、文档目录、OpenAI 兼容 Base URL 和向量维度 |
| `[voiceServiceConfig]` | 百度 TTS API Key 和 Secret Key |
| `[aiModelConfig]` | 通用 AI API Key 配置 |

OpenAI 兼容普通对话模型还会读取环境变量：

```bash
export OPENAI_API_KEY="your-api-key"
export OPENAI_MODEL_NAME="your-model-name"
export OPENAI_BASE_URL="https://api.openai.com/v1"
```

日志级别通过 `LOG_LEVEL` 控制，可选 `debug`、`info`、`warn`、`error`，默认 `info`。

## 用户身份字段

- `AccountNo` / `account_no`：系统生成的内部账号编号，数据库唯一，用于 JWT、会话归属、上传目录和日志排查，不作为登录输入。
- `Name` / `name`：用户昵称或显示名，允许重复，不能作为用户唯一标识，也不能与 `AccountNo` 混用。
- `Email` / `email`：注册邮箱，数据库唯一，用于验证码发送、邮箱重复注册校验和用户登录。
- RAG 上传文件按账号编号隔离，目录约定为 `uploads/{account_no}`，避免把昵称或邮箱写入文件路径。

## 启动依赖

### 方式一：使用已有远程中间件

如果 MySQL / Redis / RabbitMQ 已部署在另一台机器，直接把 `config/config.toml` 中对应 `host`、`port`、账号和密码改为远程服务可访问地址即可。

如果通过 SSH 隧道转发远程端口到本机，例如：

```bash
ssh -L 3306:127.0.0.1:3306 \
    -L 6379:127.0.0.1:6379 \
    -L 5672:127.0.0.1:5672 \
    user@remote-host
```

则后端配置可以使用 `127.0.0.1` 访问这些转发端口。

### 方式二：本地 Docker Compose

项目提供 `docker-compose.yml` 作为本地开发依赖，可启动 MySQL、Redis Stack、RabbitMQ：

```bash
docker compose up -d
```

默认容器连接信息：

| 服务 | 地址 | 账号 | 密码 | 说明 |
| --- | --- | --- | --- | --- |
| MySQL | `127.0.0.1:3306` | `hegang` | `hg200512hg` | 默认库名 `GopherAI` |
| Redis Stack | `127.0.0.1:6379` | - | - | 支持 RAG 依赖的 RediSearch |
| RabbitMQ | `127.0.0.1:5672` | `root` | `123456` | 管理后台 `http://127.0.0.1:15672` |

常用命令：

```bash
docker compose ps
docker compose logs -f
docker compose down
```

删除容器并清空数据卷：

```bash
docker compose down -v
```

如果后端运行在本机，请将 `config/config.toml` 中中间件地址配置为宿主机可访问地址，例如 `127.0.0.1` 或远程机器 IP。如果后端也运行在同一个 Compose 网络内，则可使用服务名 `mysql`、`redis`、`rabbitmq`。

## 启动后端

先确认 `config/config.toml` 中 MySQL、Redis、RabbitMQ 均可连接，然后启动：

```bash
go mod download
go run main.go
```

后端启动流程：

1. 加载配置与初始化日志。
2. 连接 MySQL 并执行 GORM AutoMigrate。
3. 从数据库加载历史消息到 AIHelperManager。
4. 初始化 Redis。
5. 初始化 RabbitMQ 并启动 `Message` 队列消费者。
6. 在独立 goroutine 中通过 `http.Server` 监听 `[mainConfig]` 配置的地址和端口。

后端关闭流程（优雅关闭）：

1. 主协程监听 `SIGINT`（Ctrl+C）/ `SIGTERM`（容器停止）信号。
2. 收到信号后在 10 秒超时内调用 `http.Server.Shutdown`，停止接收新请求并等待在途请求处理完成。
3. 依次关闭 RabbitMQ（消费者随连接关闭退出）、Redis、MySQL 连接，释放资源后退出。

## 工具包拆分说明

为减少 `utils` 模块职责混杂，项目已按职责拆出以下包：

- `auth`：密码哈希校验与 JWT 生成/解析
- `random`：随机数字码生成
- `id`：UUID 生成
- `fileutil`：上传文件校验、目录文件清理
- `mapper`：`model.Message` 与 `schema.Message` 的相互转换
- `utils`：当前仅保留遗留 `MD5` 接口，新增通用逻辑不再继续放入该包
- `common/aihelper/provider`：模型接口与 OpenAI / Ollama / RAG / MCP provider 实现
- `common/aihelper/session`：单会话消息历史、持久化回调与同步/流式生成协调
- `common/aihelper/factory`：根据模型类型和配置创建 provider 与 helper
- `common/aihelper/manager`：按用户/会话维度管理 helper 生命周期
- `common/aihelper`：保留兼容入口，降低上层调用改动面
- `service/tts`：TTS 业务服务层，编排 `common/tts` 并返回 `bo` 与错误码，使 controller 不再直接依赖基础设施
- `common/rag`：RAG 已按职责拆分为多个文件——`embedding.go`（向量生成器）、`document.go`（文档加载/切块）、`indexer.go`（向量索引与生命周期）、`retriever.go`（向量检索）、`prompt.go`（提示词构造）、`store.go`（`uploads/{account_no}` 文件系统约定）

当前相关调用已迁移到这些明确包中，例如用户认证、JWT 中间件、RAG 文件上传、AI 消息转换以及 AIHelper 内部职责拆分逻辑。

### 密码处理

`auth/password.go` 提供用户密码相关的基础能力：

- `HashPassword`：使用 bcrypt 对明文密码进行不可逆哈希，哈希结果包含随机盐，可存入数据库。
- `CheckPasswordHash`：登录时将用户输入的明文密码与数据库中的 bcrypt 哈希值进行比对，匹配成功返回 `true`。

业务代码不应保存或比较明文密码，也不应使用 MD5/SHA 等普通摘要算法处理登录密码。

## 启动前端

```bash
cd vue-frontend
npm install
npm run serve
```

前端开发服务器默认监听 `8080`。`vue-frontend/vue.config.js` 会把 `/api` 代理到后端 `http://localhost:9090`，并重写为 `/api/v1`。

## 接口文档

详见 `docs/API.md`。

## JSON 控制器约定

为统一 `application/json` 接口的请求绑定与响应处理，项目在 `controller/common.go` 中提供了通用辅助函数：

- `controller.BindJSON[T]`：统一完成 JSON 请求体绑定与参数校验，参数错误时直接返回标准错误响应。
- `controller.JSON`：统一输出业务成功或失败响应，减少各控制器重复拼装返回值。
- `controller.Handler(...)`：提供底层 JSON 绑定包装实现；`router` 包再通过同名 `Handler(...)` 薄包装暴露给路由注册使用，例如 `router/user.go`、`router/ai.go` 中的 `r.POST(..., Handler(controller.xxx))`。

采用这一约定后，控制器处理函数可以直接声明为接收类型化 DTO，例如 `func(c *gin.Context, req dto.LoginRequest)`，无需在每个处理函数里重复编写 `ShouldBindJSON` 和参数错误响应逻辑。

## SSE 流式约定

流式对话接口（如 `POST /api/v1/ai/chat/send-stream`）的 HTTP 传输细节由 controller 层统一接管，service 层不再依赖 `net/http`：

- `controller/sse.go` 提供 `SSEWriter` 适配器，负责写入 SSE 响应头、`data:` 帧编码、`flush`、会话 `sessionId` 首帧与 `[DONE]` 结束帧。
- `service/session` 的流式函数（`StreamMessageToExistingSession`、`ChatStreamSend`）只接收内容分片回调 `func(chunk string)`，专注驱动 AI 流式生成，不感知传输协议。
- 控制器通过 `NewSSEWriter(c)` 创建适配器，并将 `sse.Chunk()` 作为回调传入 service。

这样业务逻辑与传输协议解耦，未来若新增其他流式传输方式（如 WebSocket），只需替换适配器而无需改动 service。

## 注意事项

- RAG 文件上传只允许 `.md` 和 `.txt` 文件。
- 每次上传 RAG 文件会清理当前用户已有上传文件，并删除旧 Redis 向量索引。
- 图片识别依赖服务器上的 ONNX 模型路径 `/root/models/mobilenetv2/mobilenetv2-7.onnx` 和标签文件 `/root/imagenet_classes.txt`。
- TTS 接口依赖百度智能云语音合成配置。
- 受保护接口需要请求头 `Authorization: Bearer <token>`；JWT 中间件也兼容 URL 参数 `?token=<token>`。
