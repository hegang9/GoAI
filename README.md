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
│   │   ├── ai/             #   OpenAI/Ollama/auto 模型与工厂（auto=planner检索+ReAct工具）
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

### RAG 离线评测（watsonxDocsQA）

`cmd/ragbench` 默认读取 `dataset/watsonxDocsQA` 中的 Parquet 文件，将 1,144 篇
`md_document` 以 `{doc_id}.md` 建立一次完整向量索引，再用 QA split 的
`correct_answer_document_ids` 评估金标准文档召回率、空召回率和文档级 MRR（同一文档的
多个 chunk 只占一个排名）。`train`（45 条）
用于调参，`test`（30 条）用于最终验收；当前评测不判断最终生成答案是否匹配
`correct_answer`。

数据集目录是本地评测依赖，不纳入版本控制，目录结构应为：

```text
dataset/watsonxDocsQA/
├── corpus/train-00000-of-00001.parquet
└── question_answers/
    ├── train-00000-of-00001.parquet
    └── test-00000-of-00001.parquet
```

先执行无外部依赖的数据校验：

```bash
go run ./cmd/ragbench -validateOnly
```

完整测试会连接配置中的 Redis Stack、Embedding API，以及启用时的 reranker。
默认使用专用测试账号 `95829666279`。工具会校验 Redis 索引、语料指纹和索引期配置
指纹：首次运行、语料变化或 embedding/分块配置变化时自动重建；只有检索期配置变化时
直接复用已有索引：

```bash
# 自动判断：首次运行会建库，索引兼容时直接复用
go run ./cmd/ragbench -split test -accountNo 95829666279

# 用 train split 调整 TopK、距离阈值或 reranker 后会复用索引
go run ./cmd/ragbench -split train -accountNo 95829666279

# 显式忽略校验并强制重建
go run ./cmd/ragbench -split test -accountNo 95829666279 -reindex=true

# 只评测前 3 条进行链路冒烟测试
go run ./cmd/ragbench -split test -limit 3
```

默认门禁为金标准文档召回率不低于 `0.80`、空召回率不高于 `0.10`。
索引期配置包括 embedding 模型/BaseURL/维度、分块大小与重叠、语义切分和标题注入；
检索期配置包括 TopK、距离阈值、召回候选数、上下文窗口和 reranker。建库完成标记只在
1,144 篇文档全部索引成功后写入，并记录实际 chunk 数；上传或删除向量会先使标记失效，
途中失败、索引缺失或 chunk 数不一致都会在下次运行时自动重建。

### Planner 离线评测（PlanBench）

`cmd/planbench` 评估 Planner 的检索决策、query 改写与显式文档/章节 filter 提取；
不读取文档正文、不连接 Redis，也不评最终回答。默认加载本地
`dataset/watsonxDocsQA` 与受版本控制的 `testdata/planbench/evalset.jsonl`，按 `train`/
`test` 合并为双源评测集。首版共 135 条：45 条 watsonxDocsQA 英文单轮正例，以及 90 条
通用企业中文人工样本，整体为 67 条应检索、68 条不检索。

```bash
# 校验数据集，不调用 Planner
go run ./cmd/planbench -validateOnly -split test

# 运行完整 test split；需要可用的 [plannerConfig]
go run ./cmd/planbench -split test -accountNo planner_bench

# 运行 train split 或只运行前 3 条
go run ./cmd/planbench -split train
go run ./cmd/planbench -split test -limit 3
```

评测账号目录必须不存在或为空；工具会在 `uploads/{accountNo}` 创建并清理占位文件，
请使用专用账号。当前仅输出误判率、filter 准确率和 ROUGE-L 观察指标，不设置质量门禁。
详细格式与使用方法见 [docs/PlanBench使用说明.md](docs/PlanBench使用说明.md)。

`test/` 目录包含：

- `architecture_test.go`：领域层零框架依赖约束
- `code_test.go` / `hash_test.go` / `id_test.go` / `random_test.go` / `logger_test.go` / `fileutil_test.go`：对应 `pkg/` 工具包测试
- `storage_test.go` / `storage_hasdocs_test.go`：`internal/infrastructure/storage` 文档存储、路径安全与多文档判断测试
- `rag_document_test.go`：RAG 文本分块（含中文/重叠/非法参数）、Eino 切分器路径（递归切分不断句、Markdown 标题切分）与提示词构造测试
- `rag_parser_test.go`：RAG 文档解析（txt/md/docx）与解析+切分器分块流程测试
- `rag_engine_test.go`：RAG 召回结果的距离解析与阈值过滤测试
- `rag_reranker_test.go`：RAG 精排（reranker）HTTP 客户端的打分排序/截断/降级与最低分阈值过滤测试
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

| 文件                      | 作用                                                                                    |
| ------------------------- | --------------------------------------------------------------------------------------- |
| `.vscode/settings.json`   | Go（gofmt + gopls + staticcheck）、Vue/ESLint、保存时格式化与 import 整理、文件排除规则 |
| `.vscode/launch.json`     | 一键调试：后端（Go/Delve）、前端（Chrome + dev server）、全栈复合启动、可选 MCP 服务    |
| `.vscode/tasks.json`      | 前端 `npm run serve` 后台任务，以及统一的 `gopherai: release dev ports` 端口清理任务    |
| `.vscode/extensions.json` | 推荐扩展：Go、Volar、ESLint、Prettier、TOML 等                                          |

首次打开项目时，按提示安装推荐扩展即可。个人偏好（主题、翻译、Copilot 等）请保留在用户级 `settings.json`，不要写入工作区配置。

### 一键调试（VS Code / Cursor）

在「运行和调试」面板选择配置后按 F5：

| 配置名               | 说明                                                                                                                                                                         |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GopherAI: 后端`     | 以 Delve 调试 `cmd/server`，工作目录为项目根（读取 `config/config.toml`）；Gin 默认 debug 模式（彩色级别文本），日志双写 stdout 与 `logs/`；启动前/结束调试后自动释放 `9090` |
| `GopherAI: 前端`     | 先启动 `vue-frontend` dev server，再打开 Chrome 调试 `http://localhost:8080`；结束调试后自动释放 `8080` 等前端端口                                                           |
| `GopherAI: 全栈调试` | 同时启动后端与前端（前端代理 `/api` → `localhost:9090`）；启动前清理残留端口，停止调试后释放 `9090`/`8080` 等                                                                |
| `GopherAI: MCP 服务` | 独立调试 `cmd/mcp` 天气工具服务（`:8081`）；启动前/结束调试后自动释放 `8081`                                                                                                 |

前置条件：已安装 [Go 扩展](https://marketplace.visualstudio.com/items?itemName=golang.go) 与 Delve；前端调试需本机 Chrome。`vue-frontend` 依赖通过 `npm install` 安装。端口清理由 `.vscode/tasks.json` 调用 `.vscode/kill-ports.cmd` 完成（需 Windows，管理员权限非必须）。

## 配置说明

后端启动时读取 `config/config.toml`。关键配置如下：

| 配置段                 | 作用                                                                                                                                                                                                                                                           |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `[mainConfig]`         | 后端监听地址与端口，默认 `0.0.0.0:9090`                                                                                                                                                                                                                        |
| `[mysqlConfig]`        | MySQL 地址、账号、密码、数据库名和字符集                                                                                                                                                                                                                       |
| `[redisConfig]`        | Redis 地址、密码、DB，以及启动阶段 Ping 超时（`pingTimeoutMs`，<=0 默认 3000）                                                                                                                                                                                 |
| `[rabbitmqConfig]`     | RabbitMQ 地址、凭证和 vhost；主链路、五档延迟重试、最终 DLQ 的 exchange/queue/routing key；本地重试、抖动、prefetch 与发布确认超时                                                                                                                            |
| `[emailConfig]`        | 注册验证码邮件配置                                                                                                                                                                                                                                             |
| `[jwtConfig]`          | JWT 过期时间、签发信息和密钥                                                                                                                                                                                                                                   |
| `[ragModelConfig]`     | RAG 使用的嵌入模型名、独立 API Key、文档目录、OpenAI 兼容 Base URL、向量维度，以及检索增强参数（分块大小/重叠、TopK、距离阈值、是否启用多轮 query 改写、是否启用精排 reranker 及其召回放大/截断/最低分阈值、语义切分/上下文增强/块头标签三项分块索引升级开关） |
| `[voiceServiceConfig]` | 百度 TTS API Key 和 Secret Key                                                                                                                                                                                                                                 |
| `[autoModelConfig]`    | auto 主力模型的对话模型名、Base URL 与 API Key（需支持 tool calling）；完全自洽，独立于 RAG                                                                                                                                                                    |
| `[chatReplayConfig]`   | 会话历史回放：启动预热最近 N 个活跃会话、默认模型类型                                                                                                                                                                                                          |
| `[plannerConfig]`      | planner 检索决策器：是否启用、轻量模型名/BaseURL/APIKey、回溯窗口与超时                                                                                                                                                                                        |
| `[mcpConfig]`          | MCP 工具服务 Streamable HTTP 端点（`baseUrl`）；auto 模型懒连接拉取工具集，为空时退化为无工具纯生成                                                                                                                                                            |
| `[imageServiceConfig]` | ONNX 图像识别模型与标签文件路径（`modelPath` / `labelPath`），随部署环境变化                                                                                                                                                                                   |

RabbitMQ 适配器启动时会按配置幂等声明主链路、五档延迟重试与 DLQ 拓扑；控制台中同名对象的类型或参数不一致会导致启动失败。正常、重试和最终 DLQ 发布均复用 confirmed-publish 流程，使用持久化消息、`mandatory`、publisher confirm 和不可路由检查；消费端使用独立 Channel、显式 prefetch 与手动 ACK。首次瞬时失败按配置进行本地快速重试，仍失败时携带 `x-retry-count` 依次进入五档延迟队列，并增加 0～25% 随机抖动；确定性异常或五次延迟重试耗尽后可靠发布到最终 DLQ。重试或 DLQ 副本收到 Broker confirm 后才 ACK 原消息，系统性异常或可靠发布失败时关闭消费 Channel，使未 ACK 消息重新入队。

RabbitMQ 包的单元测试覆盖错误分类、重试 Header 解析、档位边界、抖动范围、本地快速重试、confirmed-publish 前置校验和 DLQ 消息构造；真实 Exchange/Queue 路由、TTL 回流及 ACK 时序仍需在独立测试 vhost 中执行集成测试。

### 延迟任务所有权

延迟领域模型区分调度标识与业务消息标识：`delay.Task.ID` 是一次延迟调度的 `schedule_id`，`message.Message.ID` 是跨消费者组重试保持不变的 `message_id`。任务只保存规范化后的业务 `Message`、逻辑投递 `Target`、重试次数和绝对目标时间，不再重复保存 destination/payload；`TargetTopic` 表示发布到消息原 Topic，`TargetConsumerGroup` 表示精确回投消费者组，领域层不包含 Exchange、Queue 或 AMQP 类型。

延迟任务仓储使用 MySQL 持久化小时到天级等待任务：`pending` 表示 MySQL 持有任务，Poller 通过短事务和 `FOR UPDATE SKIP LOCKED` 批量抢占到期任务，并写入 `lease_owner`、`lease_until_ms` 后进入 `dispatching`。租约让多实例不会同时处理同一条记录，也让进程崩溃后可由其他实例在租约到期后恢复任务；每次抢占和状态转换都会递增 `version`，用于拒绝上一轮投递的迟到回调。

Poller 在事务提交后才向 Level MQ 发布。只有收到 RabbitMQ Broker confirm 才调用 `MarkLevelQueued`，把任务标记为 `level_queued` 并确认 MQ 已接管；明确收到 NACK 或发送前失败时调用 `Release` 回到 `pending`。发布结果未知时不释放租约，等待过期后使用相同任务 ID 重投，以 at-least-once 的重复换取不丢失。`Cancel` 仅允许按预期版本取消仍处于 `pending` 的任务。

应用层 `internal/application/delay.Poller` 默认每 200ms 提前扫描未来 10 秒内的任务，使用固定 worker 数在 MySQL 抢占事务提交后并发发布 Level MQ。任务按剩余毫秒向下取整选择 Level 0～10；Level Publisher 只有明确返回 `PublishRejectedError` 时 Poller 才释放租约，confirm 超时、连接中断或数据库状态回写失败均保留租约等待恢复。

`[chatReplayConfig]` 示例：

```toml
[chatReplayConfig]
sessionLimit = 50            # 启动时预热的最近活跃会话数（全局）
defaultModelType = "auto"    # 启动预热与查询历史时的默认模型类型
```

`sessionLimit` 或 `defaultModelType` 未配置时，默认分别为 `50` 和 `"auto"`。

`[autoModelConfig]` 示例（auto 主力模型，独立于 RAG）：

```toml
[autoModelConfig]
modelName = "qwen-plus"      # 需支持 tool calling
baseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
apiKey = "your-api-key"
```

`[ragModelConfig]` 检索增强相关参数示例：

```toml
[ragModelConfig]
embeddingModel = "Qwen/Qwen3-Embedding-4B"   # 向量嵌入模型
apiKey = "your-api-key"                 # RAG 独立鉴权凭证（嵌入 + 重排共用）
baseUrl = "https://api.siliconflow.cn/v1"
dimension = 2048                        # 向量维度；SiliconFlow Qwen3 Embedding 会按此参数返回
chunkSize = 512                         # 单个文本块最大字符数（<=0 默认 512；递归切分按自然边界切，单块长度可能略有出入）
chunkOverlap = 64                       # 相邻块重叠字符数（<0 默认 64）
topK = 5                                # 检索返回的最相关块数（<=0 默认 5）
maxDistance = 0.6                       # COSINE 距离阈值，超出视为不相关（<=0 默认 0.6）
enableQueryRewrite = false             # 是否用 LLM 把多轮追问改写为自包含检索 query
rerankEnable = true                    # 是否启用精排：召回放大 → 交叉编码重排 → 截断 TopN
rerankModel = "Qwen/Qwen3-Reranker-4B" # SiliconFlow 重排模型
rerankBaseUrl = "https://api.siliconflow.cn/v1/rerank"  # 重排服务完整地址
recallTopK = 20                         # 启用精排时的召回候选数（粗排放大，<=0 默认 20）
rerankTopK = 5                          # 精排后保留的文档数（<=0 时沿用 topK）
rerankMinScore = 0.0                    # 精排最低相关分阈值（越大越相关），0 表示不过滤
enableSemanticChunking = false         # 是否启用语义切分（句向量相似度断点）；仅对新上传文档生效，不迁移存量索引
semanticBreakpointPercentile = 95.0    # 语义断点距离分位数阈值（0-100，越大切块越少；非法值默认 95）
semanticBufferSize = 1                 # 句向量滑窗每侧大小（>0 时用相邻句拼接稳定单句语义；<0 默认 1）
contextWindow = 0                       # 上下文增强：命中块前后各取 N 个邻居块拼接（0=关闭）
enableHeaderInjection = false          # 是否在块正文首部注入「来源｜章节」块头标签（默认关闭，仅对新文档生效）
```

> **文档分块与索引升级（灰度 / newdocs_only）**：`enableSemanticChunking`、`contextWindow`、`enableHeaderInjection` 三项均**默认关闭、保持现有行为**，开启后仅对**新上传文档**生效、**不迁移存量索引**（存量按旧 schema 优雅降级）。
>
> - **语义切分**（`enableSemanticChunking`）：非 Markdown 文件按句向量余弦距离的 `semanticBreakpointPercentile` 分位数定位语义边界断块，过短块合并、超长块二次硬切；任一步失败自动回退递归切分→定长滑窗，索引不中断。Markdown 仍走标题感知切分以保留章节结构。
> - **上下文增强**（`contextWindow`）：检索打分仍用小块保精度，命中后按确定性 key `rag_docs:{accountNo}:{storedName}:chunk_N` 取回前后各 N 个邻居块，按 chunk 序拼接、跨命中去重合并相邻 span；索引期额外写 `chunk`/`stored` HASH 字段用于定位，存量旧块无此字段时自动跳过扩展。
> - **块头标签**（`enableHeaderInjection`）：把「来源：foo.md｜章节：H1 > H2」前缀拼到块正文首部（同时进入向量与提示词），并写入 `headers` 元数据；引用展示行扩展为 `[文档 N｜来源：foo.md｜章节：H1 > H2]`。

启用精排后检索变为“**粗排（向量召回放大到 `recallTopK`）→ 精排（reranker 重排打分）→ 截断到 `rerankTopK` →（可选）按 `rerankMinScore` 兜底过滤**”两阶段流程；精排服务调用失败时自动降级为向量排序，RAG 链路不中断。`rerankEnable = false`（默认）时回到纯向量排序的现有行为。注意：`maxDistance` 是向量 **距离**（越小越相关）粗筛阈值，`rerankMinScore` 是精排 **相关分**（越大越相关）阈值，两者语义相反、不可混用。复用 `[ragModelConfig].apiKey` 作为重排服务鉴权 Key。

`[autoModelConfig]` 配置 auto 主力模型的对话模型（planner + 检索增强 + ReAct 工具），`[ragModelConfig].apiKey` 为 RAG 嵌入与重排提供独立鉴权；两段互不依赖，允许 auto 与 RAG 使用不同 provider：

```toml
[autoModelConfig]
modelName = "qwen-plus"
baseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
apiKey = "your-api-key"
```

模型连接信息不再从系统环境变量兜底读取；后端启动前请在 `config/config.toml` 中配置完整。

后端日志由 `pkg/logger` 统一初始化（`bootstrap.New` 首行调用 `InitLogger`）：

- **输出**：stdout 与 `logs/%Y-%m-%d.log` 双写；单文件超过 50MB 自动轮转，保留 30 天
- **格式**：Gin `debug` 模式为易读文本（控制台带 ANSI 颜色级别标签 `DEBUG`/`INFO`/`WARN`/`ERROR`，文件同格式无颜色）；`release` / `test` 为 JSON；F5 调试后端时 Gin 未设 `GIN_MODE`，默认为 debug
- **source**：指向业务调用方（如 `file/service.go:69`），而非 `pkg/logger` 包装层
- **级别**：由 `pkg/logger/logger.go` 中 `defaultLogLevel` 常量控制（当前为 `debug`），修改后需重新编译；不设 `LOG_LEVEL` 环境变量

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

| 服务        | 地址             | 账号     | 密码         | 说明                              |
| ----------- | ---------------- | -------- | ------------ | --------------------------------- |
| MySQL       | `127.0.0.1:3306` | `hegang` | `hg200512hg` | 默认库名 `GopherAI`               |
| Redis Stack | `127.0.0.1:6379` | -        | -            | 支持 RAG 依赖的 RediSearch        |
| RabbitMQ    | `127.0.0.1:5672` | `root`   | `123456`     | 管理后台 `http://192.168.71.109:5672` |

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
  - `ai`：OpenAI / Ollama / auto 模型实现与模型工厂，`schema.go` 负责领域消息与模型消息互转。
    - auto 模型（`auto_router.go` + `planner.go` + `retrieval_modifier.go` + `tools.go`）是统一自动编排模型：每轮先由 `Planner` 决策「是否检索」并产出 `TurnPlan`（含 query 改写与 doc_filter，取代旧 RAG 的 query rewrite / filter intent 两步），再由 `RetrievalModifier` 在进入 Agent 前做一次性检索增强（替换最后一条用户消息），最后交给 Eino `flow/agent/react` 的 ReAct Agent 生成。工具默认注入该账号可用的 MCP 工具集，由模型在生成中按需 native function calling 自主调用；检索是 pre-generation 上下文准备，不进 ReAct 的 `MessageModifier`（后者每轮触发、且循环中途最后一条是工具结果，重复检索会改错位置）。
    - MCP 工具由 `eino-ext/components/tool/mcp` 适配器从 MCP Server 批量自动转换（`mcp.GetTools`），新增工具时本层零改动；MCP 客户端采用**懒连接**，首次 `Generate`/`Stream` 才建立连接、拉取工具并构建 Agent，MCP 临时不可用时降级为纯生成（不缓存无工具 Agent，下次重试以自动恢复工具能力）。
    - Phase 3 后旧 `RAGModel`（`rag.go`）/`MCPModel`（`mcp.go`）已退役，工厂不再创建 `"2"`/`"3"`，其能力由 auto 统一承载；这两个文件暂作死代码保留，待后续手动清理。
  - `rag`：向量生成、文档加载/切块、索引、检索、提示词构造。
  - `rag`：向量生成、文档加载/切块（Eino `document.Transformer` 切分器：非 Markdown 走递归切分、`.md` 走标题感知切分，失败时回退定长滑窗）、索引、检索、提示词构造。
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
npm install    # 安装依赖（node_modules 已加入 .gitignore，勿提交到 Git）
npm run serve
```

前端开发服务器默认监听 `8080`。`vue-frontend/vue.config.js` 会把 `/api` 代理到后端 `http://localhost:9090`，并重写为 `/api/v1`。

### 前端目录结构

前端按「视图 / 组件 / 组合式逻辑 / 布局 / 共享样式」分层，UI 微调的着力点集中在样式与组件两处，避免改动散落在各视图内部：

```text
vue-frontend/src/
├── assets/styles/             # 共享视觉资产
│   ├── tokens.css             #   设计 token：颜色 / 圆角 / 阴影 / 间距 CSS 变量
│   ├── gradients.css          #   渐变背景 + 颗粒动画（供 GradientBackground 使用）
│   └── chat.css               #   会话侧栏 / 消息气泡 / 顶部条 / 输入区共享样式
├── composables/               # 可复用副作用（按职责拆分，view 只做编排）
│   ├── useChatSession.js      #   会话加载 / 切换 / 创建 / 同步历史
│   ├── useChatSend.js         #   消息发送：普通 + 流式 SSE 解析与错误回滚
│   ├── useRagFiles.js         #   RAG 文档列表与上传
│   ├── useTTS.js              #   百度 TTS 创建任务 + 轮询播放
│   └── useAutoScroll.js       #   容器滚动到底部
├── components/                # 可复用 UI 组件（纯展示 + props/events）
│   ├── GradientBackground.vue #   统一渐变 + 颗粒背景
│   ├── auth/AuthCard.vue      #   登录 / 注册共用卡片外壳
│   └── chat/                  #   聊天相关子组件
│       ├── SessionSidebar.vue #     会话列表侧栏
│       ├── ChatTopBar.vue     #     顶部工具条（返回 + 标题 + 工具插槽）
│       ├── MessageList.vue    #     消息列表容器（含自动滚动）
│       ├── MessageBubble.vue  #     单条消息气泡（markdown / TTS / 图片）
│       └── ChatInput.vue      #     文本输入区
├── layouts/
│   └── ChatLayout.vue         # 聊天页统一外壳：左侧导航 + 右侧主区
├── router/                    # 路由表与鉴权守卫
├── utils/api.js               # axios 实例与统一响应信封展平
└── views/                     # 页面级组件（仅做编排，不写网络细节）
    ├── Login.vue / Register.vue
    ├── Menu.vue               # 菜单卡片（数据驱动）
    ├── AIChat.vue             # 装配 ChatLayout + 子组件 + composables
    └── ImageRecognition.vue   # 复用 ChatLayout + MessageList
```

依赖关系：`views → layouts + components + composables → utils`，`composables` 仅依赖 `utils/api`，`components` 不依赖业务逻辑。共享样式通过 `assets/styles/*` 在 `main.js` 全局引入，并以 CSS 变量（`--gradient-brand` / `--radius-*` / `--shadow-*` 等）暴露给所有组件，调整全局视觉只需改 `tokens.css` 与 `chat.css`。

顶层 `App.vue` 通过 `router-view` 的 `route.fullPath` 为动态页面组件提供 key，配合页面过渡动画强制每次导航挂载目标页面，避免旧页面离场后停留在空节点导致跳转白屏。

### 前端验证

```bash
cd vue-frontend
npm run lint
```

### 前端诊断日志

前端在开发环境会输出带 `[GopherAI-FE]` 前缀的诊断日志，覆盖应用挂载、路由守卫、路由完成、路由错误、Vue 全局错误/警告和顶层页面过渡阶段，用于排查“页面跳转后白屏、刷新后恢复”这类运行时问题。

生产环境默认不输出；如需临时开启，可在浏览器控制台执行：

```js
localStorage.setItem("gopherai:frontend-debug", "1");
```

关闭诊断日志：

```js
localStorage.removeItem("gopherai:frontend-debug");
```

## 设计与接口文档

详见 `docs/API.md`。

- `docs/API.md`：HTTP 接口说明。
- `docs/Redis RediSearch 向量存储设计.md`：RAG 文档向量在 Redis / RediSearch 中的索引、写入、检索与持久化设计。

## JSON 控制器约定

为统一 `application/json` 接口的请求绑定与响应处理，项目在 `internal/interfaces/http/httpx` 中提供了通用辅助函数：

- `httpx.BindJSON[T]`：统一完成 JSON 请求体绑定与参数校验，参数错误时直接返回标准错误响应。
- `httpx.JSON`：统一输出业务成功或失败响应，减少各控制器重复拼装返回值。成功与失败走**同一套信封构建逻辑**（`dto.NewResponse`）：成功时 `data` 为业务数据对象，失败时无业务数据故 `data` 为 `null`。
- `httpx.Handler(...)`：底层 JSON 绑定包装实现，供 `router` 注册路由时使用，例如 `r.POST(..., httpx.Handler(h.Login))`。

采用这一约定后，控制器处理函数可以直接声明为接收类型化 DTO，例如 `func(c *gin.Context, req dto.LoginRequest)`，无需在每个处理函数里重复编写 `ShouldBindJSON` 和参数错误响应逻辑。控制器统一实现为可注入的 `Handlers` 结构体方法，依赖通过 `bootstrap` 注入。

### 统一响应信封

所有 HTTP 接口均以 `dto.Response` 信封返回，业务数据统一放入 `data` 字段（不再将状态字段平铺进各业务 DTO）：

```go
// internal/interfaces/http/dto/common.go
type Response struct {
    StatusCode code.Code `json:"status_code"`           // 业务状态码（成功为 1000）
    StatusMsg  string    `json:"status_msg,omitempty"`  // 状态码对应文案
    Data       any       `json:"data"`                  // 业务数据；失败或无数据时为 null
}
```

- 成功：`{ "status_code": 1000, "status_msg": "success", "data": { "token": "xxx" } }`
- 失败：`{ "status_code": 2004, "status_msg": "邮箱或密码错误", "data": null }`

控制器只需构造**纯业务数据 DTO**（不再内嵌 `dto.Response`），由 `httpx.JSON` 包装进信封，例如 `httpx.JSON(c, &dto.LoginResponse{Token: token}, errCode)`；无业务数据的接口（如验证码）直接传 `nil`。`data` 字段固定存在（无 `omitempty`），前端可稳定基于 `status_code === 1000` 判断成功并从 `data` 取业务数据。

## SSE 流式约定

流式对话接口（如 `POST /api/v1/ai/chat/send-stream`）的 HTTP 传输细节由接口层统一接管，应用层与领域层不依赖 `net/http`：

- `internal/interfaces/http/sse` 提供 `Writer` 适配器，负责写入 SSE 响应头、`data:` 帧编码、`flush`、会话 `sessionId` 首帧与 `[DONE]` 结束帧。
- 应用层的流式用例只接收内容分片回调 `func(chunk string)`（对应领域 `chat.StreamCallback`），专注驱动 AI 流式生成，不感知传输协议。
- 控制器创建 SSE 适配器，并将其分片写入函数作为回调传入应用服务。

这样业务逻辑与传输协议解耦，未来若新增其他流式传输方式（如 WebSocket），只需替换适配器而无需改动应用与领域层。

## 注意事项

- RAG 文件上传允许 `.md` / `.txt` / `.pdf` / `.docx` 文件；上传后先抽取纯文本，再用 Eino `document.Transformer` 切分器切块并写入向量索引：`.md` 走 Markdown 标题感知切分（保留标题层级到元数据），其余走递归切分（按段落/换行/中英文句末标点等自然边界递归，避免从句中硬断），切分器创建或执行失败时自动回退到原定长滑窗实现。切块结果仍以 `chunk_N` 编号并回填 `source`，与既有索引/检索完全兼容（PDF 扫描件无法抽取文本，会因内容为空而上传失败）。变更切分策略后，建议对存量知识库重新建索引以保持分块一致。
- RAG 已支持多文档知识库：上传为追加，不再清理已有文档；向量索引按账号聚合（`rag_docs:{accountNo}:idx`），单个文档以 `rag_docs:{accountNo}:{storedName}:` 前缀存储，可按文档粒度删除。
- 检索阶段会按 `topK` 召回并用 `maxDistance` 过滤不相关结果；过滤后为空（或账号无文档）时自动跳过检索增强，走普通对话，避免污染闲聊。
- `rerankEnable = true` 时启用两阶段检索：召回放大到 `recallTopK`、距离粗筛后交由 reranker 精排重排、截断到 `rerankTopK`，并可按 `rerankMinScore` 兜底过滤；精排结果的相关分写入文档 `rerank_score` 元数据。精排器未注入或调用失败时自动降级为向量排序，不中断链路。
- `enableQueryRewrite = true` 时，多轮对话会先用 LLM 把追问改写为自包含检索 query，改写失败自动回退到原文。
- `enableSemanticChunking = true` 时，非 Markdown 文件按句向量相似度断点做语义切分（`semanticBreakpointPercentile` 控制断块密度、`semanticBufferSize` 控制句向量滑窗），过短块合并、超长块按 `chunkSize` 二次硬切；embedding 报错/句子过少/空结果时自动回退递归切分→定长滑窗。Markdown 始终走标题感知切分。复用 Engine 内缓存的 Embedder，无需额外配置。
- `contextWindow > 0` 时启用上下文增强（small-to-big）：检索/打分仍用小块保精度，命中后按确定性 key 取回前后各 N 个邻居块，按 chunk 序拼接并跨命中去重合并相邻 span 后交 `BuildPrompt`。索引期额外写入 `chunk`/`stored` 普通 HASH 字段用于定位（不改 `FT.CREATE` schema），存量旧文档无此字段时安全跳过、不触发扩展。
- `enableHeaderInjection = true` 时，把「来源：foo.md｜章节：H1 > H2」前缀拼到块正文首部（同时进入向量与提示词）并写入 `headers` 元数据；引用展示行扩展为 `[文档 N｜来源：foo.md｜章节：H1 > H2]`，非 Markdown 文件以文件名兜底。
- 上述三项分块索引升级均**默认关闭、互不强耦合**，遵循 `newdocs_only` 灰度：开启后仅对新上传文档生效，不迁移存量索引，存量按旧 schema 优雅降级。
- 三项升级的关键节点均有结构化日志：索引入口记录开关状态，语义切分记录选中/开始/断点/硬切/降级，块头标签记录注入块数，上下文增强记录扩展前后数量与邻居读取统计；邻居块 key 级追踪使用 `Debug` 日志（当前 `defaultLogLevel` 为 `debug` 时可见）。
- 图片识别依赖服务器上的 ONNX 模型路径 `/root/models/mobilenetv2/mobilenetv2-7.onnx` 和标签文件 `/root/imagenet_classes.txt`。
- TTS 接口依赖百度智能云语音合成配置。
- 受保护接口需要请求头 `Authorization: Bearer <token>`；JWT 中间件也兼容 URL 参数 `?token=<token>`。
