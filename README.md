# GopherAI

GopherAI 是一个 Go + Vue 的 AI 应用示例，后端基于 Gin，前端基于 Vue。当前功能包括用户注册登录、AI 对话、流式对话、RAG 多文档知识库（支持 md/txt/pdf/docx，分块向量检索）、图片识别和百度 TTS 语音合成。

## 项目结构

项目采用「分层架构 + 端口与适配器（Ports & Adapters）」组织，依赖方向统一指向领域层（`internal/domain`）：

```text
.
├── cmd/
│   ├── server/             # 后端主入口（装配 + 生命周期编排）
│   └── mcp/                # 独立的 MCP 天气服务/客户端（单独 Go module）
├── config/                 # 配置加载与 config.toml（按工作目录读取）
├── internal/
│   ├── domain/             # 领域层：实体、值对象、聚合与端口接口（无外部依赖）
│   │   ├── chat/           #   会话领域：Message/Session、Conversation 聚合、Manager、Model/Sink 等端口
│   │   ├── user/           #   用户领域：User 实体、Repository/PasswordHasher/TokenIssuer/CaptchaStore/Mailer 端口
│   │   ├── rag/ storage/ image/ tts/  # 其余领域端口
│   ├── application/        # 应用层：用例编排（chat/user/file/image/tts），仅依赖领域端口
│   ├── infrastructure/     # 基础设施层：端口的具体实现（适配器）
│   │   ├── persistence/    #   MySQL + GORM（PO 与仓储实现）
│   │   ├── cache/redis/    #   Redis 验证码存储与向量索引
│   │   ├── mq/rabbitmq/    #   消息发布（MessageSink）与消费落库
│   │   ├── ai/             #   OpenAI/Ollama/RAG/MCP 模型与工厂
│   │   ├── rag/ storage/ security/ email/ image/ tts/  # 其余适配器
│   └── interfaces/http/    # 接口层：router/controller/dto/middleware/sse/httpx
│   └── bootstrap/          # 组合根：自上而下依赖装配与启停
├── pkg/                    # 跨层通用工具：logger/code/random/id/fileutil/hash
├── vue-frontend/           # Vue 前端
├── docker-compose.yml      # 本地开发中间件编排
├── docs/API.md             # 接口文档
└── 阅读顺序.md              # 项目阅读任务清单
```

依赖规则：`interfaces → application → domain`，`infrastructure → domain`（实现端口），`bootstrap` 负责把基础设施适配器注入应用与接口层。领域层不依赖 gin / gorm / eino / redis / rabbitmq 等任何框架，该约束由 `test/architecture_test.go` 固化校验。

## 测试

```bash
go test ./test/... -v
```

`test/` 目录包含：

- `architecture_test.go`：领域层零框架依赖约束
- `code_test.go` / `hash_test.go` / `id_test.go` / `random_test.go` / `logger_test.go` / `fileutil_test.go`：对应 `pkg/` 工具包测试
- `storage_test.go` / `storage_hasdocs_test.go`：`internal/infrastructure/storage` 文档存储、路径安全与多文档判断测试
- `rag_document_test.go`：RAG 文本分块（含中文/重叠/非法参数）与提示词构造测试
- `rag_parser_test.go`：RAG 文档解析（txt/md/docx）与解析+分块流程测试
- `rag_engine_test.go`：RAG 召回结果的距离解析与阈值过滤测试
- `file_delete_test.go`：RAG 文档列出/批量删除应用服务与按文档删除存储的测试

## 环境要求

- Go：见 `.go-version` / `.tool-versions`
- Node.js 与 npm：用于运行 `vue-frontend`
- MySQL 8.x：用于用户、会话和消息持久化
- Redis Stack：普通验证码缓存 + RAG RediSearch 向量索引，启动阶段会带超时执行 Ping 校验连接；标准 Redis 不包含 RediSearch
- RabbitMQ：用于异步消息队列，默认队列名 `Message`
- 可选外部服务：OpenAI 兼容模型、阿里百炼 Embedding/Chat、百度 TTS、ONNX 图片识别模型

## 编辑器配置（VS Code / Cursor）

项目根目录提供团队共享的工作区配置：

| 文件 | 作用 |
| --- | --- |
| `.vscode/settings.json` | Go（gofmt + gopls + staticcheck）、Vue/ESLint、保存时格式化与 import 整理、文件排除规则 |
| `.vscode/extensions.json` | 推荐扩展：Go、Volar、ESLint、Prettier、TOML 等 |

首次打开项目时，按提示安装推荐扩展即可。个人偏好（主题、翻译、Copilot 等）请保留在用户级 `settings.json`，不要写入工作区配置。

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
| `[ragModelConfig]` | RAG 使用的模型名、文档目录、OpenAI 兼容 Base URL、向量维度，以及检索增强参数（分块大小/重叠、TopK、距离阈值、是否启用多轮 query 改写） |
| `[voiceServiceConfig]` | 百度 TTS API Key 和 Secret Key |
| `[aiModelConfig]` | 通用 AI API Key 配置 |
| `[chatReplayConfig]` | 会话历史回放：启动预热最近 N 个活跃会话、默认模型类型 |

`[chatReplayConfig]` 示例：

```toml
[chatReplayConfig]
sessionLimit = 50          # 启动时预热的最近活跃会话数（全局）
defaultModelType = "1"     # 启动预热与查询历史时的默认模型类型
```

`sessionLimit` 或 `defaultModelType` 未配置时，默认分别为 `50` 和 `"1"`。

`[ragModelConfig]` 检索增强相关参数示例：

```toml
[ragModelConfig]
embeddingModel = "text-embedding-v4"   # 向量嵌入模型
chatModelName = "qwen-turbo"            # RAG 对话模型
baseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
dimension = 1024                        # 向量维度，需与嵌入模型匹配
chunkSize = 512                         # 单个文本块最大字符数（<=0 默认 512）
chunkOverlap = 64                       # 相邻块重叠字符数（<0 默认 64）
topK = 5                                # 检索返回的最相关块数（<=0 默认 5）
maxDistance = 0.6                       # COSINE 距离阈值，超出视为不相关（<=0 默认 0.6）
enableQueryRewrite = false             # 是否用 LLM 把多轮追问改写为自包含检索 query
```

RAG 嵌入 / 对话模型的 API Key 优先取 `[aiModelConfig]` 的 `apiKey`，为空时回退到环境变量 `OPENAI_API_KEY`。

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
- 注册时会对随机生成的 `AccountNo` 做有限次数唯一性重试，并使用进程内缓存减少重复数据库查询；数据库唯一索引仍是最终一致性保障。
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
go run ./cmd/server
```

后端启动流程（全部在 `internal/bootstrap` 中显式装配，不再使用全局单例）：

1. 初始化日志、加载配置。
2. 连接 MySQL 并执行 GORM AutoMigrate，构建用户/会话/消息仓储。
3. 连接 Redis，构建验证码存储与向量索引存储。
4. 构建 RAG 引擎与 AI 模型工厂，连接 RabbitMQ 并创建消息发布器（作为会话消息的持久化 Sink）。
5. 构建会话领域管理器，并按策略 B 回放最近 N 个活跃会话到内存（`persist=false`，不重复落库）；其余会话在访问时由应用层按需懒加载。
6. 启动 `Message` 队列消费者，将队列消息通过仓储落库。
7. 装配应用服务、接口处理器与路由，在独立 goroutine 中通过 `http.Server` 监听 `[mainConfig]` 地址端口。

后端关闭流程（优雅关闭，由 `App.Shutdown` 负责）：

1. 主协程监听 `SIGINT`（Ctrl+C）/ `SIGTERM`（容器停止）信号。
2. 收到信号后在 10 秒超时内调用 `http.Server.Shutdown`，停止接收新请求并等待在途请求处理完成。
3. 依次关闭 RabbitMQ（消费者随连接关闭退出）、Redis、MySQL 连接，释放资源后退出。

## 分层职责说明

各层职责与关键包：

- **领域层 `internal/domain`**：定义业务实体与端口接口，不含任何框架依赖。
  - `chat`：`Message`/`Session` 值对象、`Conversation` 聚合（追加消息并驱动模型生成）、`Manager` 领域服务（按用户/会话维度管理会话），以及 `Model`/`ModelFactory`/`MessageSink`/`MessageRepository`/`SessionRepository` 等端口。
  - `user`：`User` 实体与 `Repository`/`PasswordHasher`/`TokenIssuer`/`CaptchaStore`/`Mailer` 端口。
  - `rag`/`storage`/`image`/`tts`：RAG 索引、用户文档存储、图片识别、语音合成等端口。
- **应用层 `internal/application`**：用例编排（`user`/`chat`/`file`/`image`/`tts`），只依赖领域端口，输出应用级结果类型。
- **基础设施层 `internal/infrastructure`**：端口的具体实现（适配器）。
  - `persistence`：GORM 持久化对象（PO，仅 `gorm` 标签映射表结构）与用户/会话/消息仓储实现；对外 JSON 由 `interfaces/http/dto` 定义。
  - `cache/redis`：验证码存储与 RAG 向量索引存储。
  - `mq/rabbitmq`：消息发布器（实现 `MessageSink`）与消费落库。
  - `ai`：OpenAI / Ollama / RAG / MCP 模型实现与模型工厂，`schema.go` 负责领域消息与模型消息互转。
  - `rag`：向量生成、文档加载/切块、索引、检索、提示词构造。
  - `security`：bcrypt 密码哈希与 JWT 签发/解析；`email`/`image`/`tts`/`storage` 为其余适配器。
- **接口层 `internal/interfaces/http`**：`router`/`controller`/`dto`/`middleware`/`sse`/`httpx`，负责协议绑定与响应。
- **组合根 `internal/bootstrap`**：把基础设施适配器注入应用与接口层，并管理启停。
- **跨层工具 `pkg`**：`logger`/`code`/`random`/`id`/`fileutil`/`hash`，不含业务逻辑。
  - `fileutil.ValidatePath`：校验目标路径是否位于基准目录内，防止 `../` 路径逃逸。
  - `fileutil.RemoveAllFilesInDir`：清理目录内普通文件时校验条目名并跳过符号链接。
  - `storage.UserDocDir` / `LocalDocStorage.Save`：对 `accountNo` 与 `storedName` 做 `filepath.IsLocal` 与路径约束校验。

### 密码处理

`internal/infrastructure/security/password.go` 提供用户密码相关的基础能力：

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

为统一 `application/json` 接口的请求绑定与响应处理，项目在 `internal/interfaces/http/httpx` 中提供了通用辅助函数：

- `httpx.BindJSON[T]`：统一完成 JSON 请求体绑定与参数校验，参数错误时直接返回标准错误响应。
- `httpx.JSON`：统一输出业务成功或失败响应，减少各控制器重复拼装返回值。
- `httpx.Handler(...)`：底层 JSON 绑定包装实现，供 `router` 注册路由时使用，例如 `r.POST(..., httpx.Handler(h.Login))`。

采用这一约定后，控制器处理函数可以直接声明为接收类型化 DTO，例如 `func(c *gin.Context, req dto.LoginRequest)`，无需在每个处理函数里重复编写 `ShouldBindJSON` 和参数错误响应逻辑。控制器统一实现为可注入的 `Handlers` 结构体方法，依赖通过 `bootstrap` 注入。

## SSE 流式约定

流式对话接口（如 `POST /api/v1/ai/chat/send-stream`）的 HTTP 传输细节由接口层统一接管，应用层与领域层不依赖 `net/http`：

- `internal/interfaces/http/sse` 提供 `Writer` 适配器，负责写入 SSE 响应头、`data:` 帧编码、`flush`、会话 `sessionId` 首帧与 `[DONE]` 结束帧。
- 应用层的流式用例只接收内容分片回调 `func(chunk string)`（对应领域 `chat.StreamCallback`），专注驱动 AI 流式生成，不感知传输协议。
- 控制器创建 SSE 适配器，并将其分片写入函数作为回调传入应用服务。

这样业务逻辑与传输协议解耦，未来若新增其他流式传输方式（如 WebSocket），只需替换适配器而无需改动应用与领域层。

## 注意事项

- RAG 文件上传允许 `.md` / `.txt` / `.pdf` / `.docx` 文件；上传后会按 `chunkSize`/`chunkOverlap` 分块并写入向量索引（PDF 扫描件无法抽取文本，会因内容为空而上传失败）。
- RAG 已支持多文档知识库：上传为追加，不再清理已有文档；向量索引按账号聚合（`rag_docs:{accountNo}:idx`），单个文档以 `rag_docs:{accountNo}:{storedName}:` 前缀存储，可按文档粒度删除。
- 检索阶段会按 `topK` 召回并用 `maxDistance` 过滤不相关结果；过滤后为空（或账号无文档）时自动跳过检索增强，走普通对话，避免污染闲聊。
- `enableQueryRewrite = true` 时，多轮对话会先用 LLM 把追问改写为自包含检索 query，改写失败自动回退到原文。
- 图片识别依赖服务器上的 ONNX 模型路径 `/root/models/mobilenetv2/mobilenetv2-7.onnx` 和标签文件 `/root/imagenet_classes.txt`。
- TTS 接口依赖百度智能云语音合成配置。
- 受保护接口需要请求头 `Authorization: Bearer <token>`；JWT 中间件也兼容 URL 参数 `?token=<token>`。
