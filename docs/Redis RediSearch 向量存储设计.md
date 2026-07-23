# Redis RediSearch 向量存储设计

## 背景

GopherAI 的 RAG 功能会把用户上传的知识文档解析为纯文本、切分为多个文本块，再通过 embedding 模型转换为语义向量。项目使用 Redis Stack 中的 RediSearch 保存和检索这些向量：Redis Hash 保存文本块、元数据和向量字段，RediSearch 在 `vector` 字段上建立向量索引，并在用户提问时按语义相似度召回相关文本块。

重构后的 RAG 引擎从“单文档单索引”调整为“账号级知识库”模型：同一账号下可以上传多个文档，多个文档共享一个账号级 RediSearch 索引；每个文档的向量 key 通过文档级前缀隔离，支持按文档粒度删除。

## 设计目标

- 支持同一账号维护多个 RAG 文档，而不是每次上传都清空旧文档。
- 以账号为单位建立 RediSearch 索引，减少索引数量，并支持账号知识库内跨文档检索。
- 以文档为单位组织 Redis key，便于删除指定文档的全部 chunk 向量。
- 在文档解析、切块、检索阈值、TopK 和 query 改写上提供可配置能力。
- 领域层只依赖 `domain/rag.Indexer` 端口，不直接依赖 Redis、RediSearch、文件系统或 eino。
- RAG 检索失败或无相关内容时回退到普通对话，避免错误上下文污染模型回答。

## 组件职责

### `VectorStore`

`VectorStore` 位于 `internal/infrastructure/cache/redis/vector.go`，负责 Redis 客户端复用和 RediSearch 命名约定。它不负责文档解析、embedding 或业务编排。

关键方法：

- `Client()`：返回底层 `*redis.Client`，供 eino Redis indexer / retriever 使用。
- `IndexName(accountNo)`：生成账号级索引名，格式为 `rag_docs:{accountNo}:idx`。
- `AccountPrefix(accountNo)`：生成账号级 key 前缀，格式为 `rag_docs:{accountNo}:`，同时作为 RediSearch `PREFIX` 和 indexer `KeyPrefix`。
- `DocKeyPrefix(accountNo, storedName)`：生成文档级 key 前缀，格式为 `rag_docs:{accountNo}:{storedName}:`。
- `InitIndex(ctx, accountNo, dimension)`：为账号创建 RediSearch 向量索引，已存在时跳过。
- `DeleteDocVectors(ctx, accountNo, storedName)`：通过 `SCAN` 匹配文档级前缀并分批删除该文档的向量 Hash。
- `DeleteIndex(ctx, accountNo)`：通过 `FT.DROPINDEX ... DD` 删除账号级索引及其关联数据。

### `rag.Engine`

`Engine` 位于 `internal/infrastructure/rag/engine.go`，实现领域层 `domain/rag.Indexer` 端口，同时提供检索能力给 `RAGModel`。

重构后，`Engine` 在 `NewEngine` 阶段完成以下工作：

- 归一化运行配置，例如 `chunkSize`、`chunkOverlap`、`topK`、`maxDistance`。
- 创建并缓存 embedding 客户端，索引端和检索端共用，避免每次上传或对话都重新创建。
- 保存 `VectorStore` 引用，用于后续创建索引、写入向量和检索。

运行期主要方法：

- `Index(ctx, accountNo, storedName, localPath)`：解析文件、切块、向量化，并写入当前账号的向量库。
- `Delete(ctx, accountNo, storedName)`：删除某个文档的全部向量数据，保留账号索引。
- `DeleteAll(ctx, accountNo)`：删除某个账号的整个 RediSearch 索引和关联数据。
- `Retrieve(ctx, accountNo, query)`：在账号知识库中检索相关 chunk，按距离阈值过滤后构造增强 prompt。

### `document.go` 与 `parser.go`

`internal/infrastructure/rag/parser.go` 负责按文件类型解析文本：

- `.txt` / `.md` / 空扩展名：按普通 UTF-8 文本读取。
- `.pdf`：使用 `ledongthuc/pdf` 抽取页面纯文本。
- `.docx`：解析 OOXML 包中的 `word/document.xml`，抽取正文文本。
- 未知扩展名：回退为普通文本读取。

`internal/infrastructure/rag/document.go` 负责文本切块和 prompt 构造：

- `SplitIntoChunks` 按 rune 数切块，支持相邻块重叠。
- `LoadDocuments` 将文件解析结果切为多个 `schema.Document`，每个 chunk 一个 document。
- `BuildPrompt` 将检索结果拼成带来源引用的中文提示词。

### `RAGModel`

`RAGModel` 位于 `internal/infrastructure/ai/rag.go`，在普通聊天模型外层叠加检索增强。

处理流程：

1. 先用 `storage.HasUserDocs(accountNo)` 判断账号是否存在上传文档；没有文档时直接走普通对话。
2. 取最后一条用户消息作为检索 query。
3. 如果启用 `enableQueryRewrite` 且存在多轮上下文，则用 LLM 将追问改写成自包含 query。
4. 调用 `Engine.Retrieve(ctx, accountNo, query)` 检索账号知识库。
5. 如果返回 `hasContext=false`，说明无相关内容，不注入上下文。
6. 如果返回 `hasContext=true`，用增强 prompt 替换最后一条用户消息，再交给聊天模型生成回答。

## 配置项

RAG 相关配置位于 `config/config.toml` 的 `[ragModelConfig]`：

```toml
[ragModelConfig]
embeddingModel = "text-embedding-v4"
apiKey = "your-api-key"
docDir = "./docs"
baseUrl = "https://dashscope.aliyuncs.com/compatible-mode/v1"
dimension = 1024
chunkSize = 512
chunkOverlap = 64
topK = 5
maxDistance = 0.6
enableQueryRewrite = false
```

配置含义：

- `embeddingModel`：用于生成文档向量和 query 向量的 embedding 模型。
- `apiKey`：RAG 独立鉴权凭证（嵌入 + 重排服务共用），独立于 auto 模型的 `[autoModelConfig].apiKey`。
- `baseUrl`：embedding 接口的 OpenAI 兼容地址。
- `dimension`：向量维度，必须与 embedding 模型输出维度一致。
- `chunkSize`：单个文本块最大字符数，按 rune 计算；非法值运行时回退到 512。
- `chunkOverlap`：相邻文本块重叠字符数；非法值运行时回退到 64，且会被限制为小于 `chunkSize`。
- `topK`：检索返回的候选文本块数量；非法值运行时回退到 5。
- `maxDistance`：COSINE 距离阈值，距离越小越相关；大于阈值的结果会被丢弃，非法值运行时回退到 0.6。
- `enableQueryRewrite`：是否在多轮对话中用 LLM 改写检索 query。

API Key 取配置中的 `RagModelConfig.RagAPIKey`（RAG 独立），不再从系统环境变量兜底读取。

## Redis 数据模型

### 命名规则

重构后的命名以账号为主轴：

```text
账号级索引名：  rag_docs:{accountNo}:idx
账号级 key 前缀：rag_docs:{accountNo}:
文档级 key 前缀：rag_docs:{accountNo}:{storedName}:
最终向量 key：  rag_docs:{accountNo}:{storedName}:chunk_N
```

其中：

- `accountNo` 来自登录用户身份，用于账号知识库隔离。
- `storedName` 是上传时生成的 UUID 文件名加原始扩展名，不直接使用用户原始文件名。
- `chunk_N` 来自 `LoadDocuments` 生成的 chunk ID，例如 `chunk_0`、`chunk_1`。

### RediSearch 索引结构

每个账号只有一个 RediSearch 索引：

```text
FT.CREATE rag_docs:{accountNo}:idx
ON HASH
PREFIX 1 rag_docs:{accountNo}:
SCHEMA
  content  TEXT
  metadata TEXT
  vector   VECTOR FLAT 6 TYPE FLOAT32 DIM {dimension} DISTANCE_METRIC COSINE
```

字段说明：

- `content`：文本块原文，作为返回给 prompt 的上下文字段。
- `metadata`：当前保存来源文件名，便于回答时标注引用来源。
- `vector`：由 `content` 生成的 FLOAT32 embedding 向量。

### Hash 数据结构

每个 chunk 写入一个 Redis Hash：

```text
key: rag_docs:{accountNo}:{storedName}:chunk_0

content  = "文本块内容"
metadata = "{storedName}"
vector   = "<FLOAT32 binary embedding>"
```

`metadata` 字段目前主要保存 `source`，也就是本地文件路径的 basename。`LoadDocuments` 还会在内存中的 `schema.Document.MetaData` 里记录 `chunk` 序号，但当前 `DocumentToHashes` 只写入了 `metadata` 字段，没有把 `chunk` 单独落到 Redis 字段中。

## 上传与写入流程

上传入口由 `internal/application/file.Service.UploadRagFile` 编排：

```text
用户上传文档
  -> 校验扩展名（.md / .txt / .pdf / .docx）
  -> 生成 storedName = uuid + 原始扩展名
  -> 保存文件到账号目录
  -> Engine.Index(accountNo, storedName, localPath)
  -> 失败时回滚本地文件，并删除该文档向量
```

当前上传是追加语义，不再清空账号旧文档。也就是说，同一账号可以上传多个文档，它们共享同一个账号级索引。

`Engine.Index` 内部流程：

```text
InitIndex(accountNo)
  -> FT.INFO rag_docs:{accountNo}:idx
  -> 索引存在：跳过创建
  -> Unknown index name：FT.CREATE

LoadDocuments(localPath, chunkSize, chunkOverlap)
  -> ParseFile 解析纯文本
  -> SplitIntoChunks 切分 chunk
  -> 每个 chunk 转为 schema.Document

redisIndexer.Store(docs)
  -> content 字段写原文
  -> content 通过 EmbedKey=vector 生成向量
  -> vector 字段写 embedding
```

写入时的 `DocumentToHashes` 关键映射：

```text
Key = "{storedName}:{doc.ID}"

content:
  Value    = doc.Content
  EmbedKey = "vector"

metadata:
  Value = doc.MetaData["source"]
```

因为 indexer 的 `KeyPrefix` 是 `rag_docs:{accountNo}:`，所以最终 key 为：

```text
rag_docs:{accountNo}:{storedName}:{chunk_N}
```

## 检索与 RAG 路由流程

用户选择 RAG 模型进行对话时，`RAGModel.buildRAGMessages` 会先判断账号是否存在上传文档。没有文档时不访问 RediSearch，直接保留原始消息。

有文档时，检索流程如下：

```text
最后一条用户消息
  -> 可选 query rewrite
  -> Engine.Retrieve(accountNo, query)
  -> redisRetriever.Retrieve
  -> FilterByDistance(maxDistance)
  -> 无相关 chunk：回退原始消息
  -> 有相关 chunk：BuildPrompt 注入参考文档
```

retriever 配置：

```text
Index        = rag_docs:{accountNo}:idx
VectorField  = vector
TopK         = cfg.TopK
ReturnFields = content, metadata, distance
Dialect      = 2
Embedding    = Engine 初始化时缓存的 embedder
```

`Retrieve` 返回三个值：

```go
prompt string
hasContext bool
err error
```

- `hasContext=true`：存在通过距离阈值过滤的相关文本块，`prompt` 是增强提示词。
- `hasContext=false`：无索引、无召回结果或全部结果超过距离阈值，调用方应使用原始用户消息。
- `err != nil`：检索链路异常，调用方记录 warn 并回退原始消息。

## Query 改写

多轮对话中，用户经常会问“它呢？”、“上面那个怎么配置？”这类依赖历史的追问。若直接用最后一句检索，召回质量会很差。

当 `enableQueryRewrite=true` 且消息数大于 1 时，`RAGModel` 会取最近 6 条消息，要求 LLM 将最后一句改写为自包含检索 query：

```text
你是检索 query 改写器。请根据下面的多轮对话历史，把用户的最后一句改写成一个语义自包含、可独立用于文档检索的查询。
```

改写失败或返回空字符串时，会回退到最后一条用户消息原文，保证检索仍可继续。

## 删除与重建

### 删除单个文档

`DeleteRagFiles` 支持批量删除账号下的部分文档，单次最多 50 个文件名。每个文件删除时先删向量，再删本地文件：

```text
DeleteRagFiles(accountNo, filenames)
  -> 去重并取 basename
  -> Engine.Delete(accountNo, storedName)
  -> VectorStore.DeleteDocVectors(accountNo, storedName)
  -> storage.RemoveUserDoc(accountNo, storedName)
```

`DeleteDocVectors` 不使用阻塞性的 `KEYS`，而是通过 `SCAN` 分批匹配：

```text
SCAN cursor MATCH rag_docs:{accountNo}:{storedName}:* COUNT 200
DEL matched_keys...
```

这样可以只删除某个文档的 chunk 向量，同时保留账号级索引和其他文档的数据。

### 删除整个账号索引

`VectorStore.DeleteIndex(accountNo)` 使用：

```text
FT.DROPINDEX rag_docs:{accountNo}:idx DD
```

`DD` 表示删除索引时同步删除被该索引关联的文档数据。该方法用于彻底清空某账号知识库；索引不存在时视为成功。

## 持久化策略

项目代码没有给 RAG 文档 Hash 设置 TTL，因此这些向量数据不会像验证码一样自动过期。它们是否能跨 Redis 重启保留，取决于 Redis 服务端配置：

- 开启 RDB 快照或 AOF 日志，并正确挂载 Redis 数据目录时，Hash 和向量数据可以随 Redis 恢复。
- 未开启持久化时，Redis 进程重启、容器重建或数据目录丢失会导致 RAG 向量数据丢失。
- 如果 Redis 配置了内存淘汰策略，内存压力下 RAG 向量数据可能被淘汰。

生产环境建议：

- 使用 Redis Stack，或确保 Redis 已加载 RediSearch 模块。
- 开启 AOF 或 RDB，且为 Redis 数据目录配置持久化卷。
- 谨慎设置 `maxmemory-policy`，避免账号知识库数据被意外淘汰。
- 如果允许数据重建，需要保证本地上传文档也有可靠持久化。

## 运维验证

以下命令中的 `{accountNo}` 和 `{storedName}` 替换为实际值。

检查账号索引：

```bash
FT.INFO rag_docs:{accountNo}:idx
```

扫描账号下全部 RAG 向量 key：

```bash
SCAN 0 MATCH rag_docs:{accountNo}:* COUNT 100
```

扫描某个文档的全部 chunk：

```bash
SCAN 0 MATCH rag_docs:{accountNo}:{storedName}:* COUNT 100
```

查看某个 chunk Hash：

```bash
HGETALL rag_docs:{accountNo}:{storedName}:chunk_0
```

确认 RediSearch 模块：

```bash
MODULE LIST
```

如果上传成功但检索无结果，优先检查：

- `FT.INFO rag_docs:{accountNo}:idx` 是否存在。
- Redis key 是否匹配账号级 `PREFIX`：`rag_docs:{accountNo}:`。
- 文档 key 是否符合 `rag_docs:{accountNo}:{storedName}:chunk_N`。
- `vector` 字段是否存在。
- `[ragModelConfig].dimension` 是否与 embedding 模型输出维度一致。
- `maxDistance` 是否过小，导致 TopK 结果全部被过滤。
- 写入端和检索端是否使用同一个 embedding 模型与 API Key。

## 已知限制与改进方向

- 当前索引算法使用 `FLAT`，适合小规模精确检索；当账号知识库 chunk 数量增长后，可评估 `HNSW` 提升检索性能。
- 当前 Redis Hash 只写入 `content`、`metadata`、`vector` 三个字段，chunk 序号未单独落库；如果需要更精确引用，可增加 `source`、`chunk`、`storedName` 等结构化字段。
- 当前 `metadata` 是 `TEXT` 字段，适合简单返回；如果后续需要按文件过滤或排序，可考虑增加 `TAG` / `NUMERIC` 字段。
- `maxDistance` 是全局阈值，不同 embedding 模型、文档类型和语言分布可能需要不同调参。
- `enableQueryRewrite` 会额外调用一次聊天模型，能改善多轮追问检索质量，但会增加延迟和成本。
- PDF 解析对扫描件无 OCR 能力，扫描型 PDF 可能解析为空文本并导致索引失败。
