# AGENTS.md

## 模块职责

- 本目录负责 Redis 连接、验证码存储和 RAG 向量索引底座。

## 变更约束

- 保持启动阶段 `Ping` 校验和 RESP2 协议设置，避免把运行期连接问题延后暴露。
- `CaptchaStore` 与 `VectorStore` 虽共享连接，但职责不要混淆；不要把用户业务语义塞进 Redis 基础操作层。
- 向量索引 key、前缀、字段名变更要同步检查 `internal/infrastructure/rag` 的索引写入和邻居块定位逻辑。
- 删除或重建索引相关逻辑必须继续支持账号级隔离，避免跨账号污染数据。
- 继续维持既有命名契约：索引名 `rag_docs:{accountNo}:idx`，chunk key 形如 `rag_docs:{accountNo}:{storedName}:chunk_N`。
- 文档级删除继续使用 `SCAN` 这类渐进式策略，不要为了方便退化成阻塞式全量 key 扫描。

## 验证

- 修改后运行 `go test ./test/... -v`，并重点关注 `test/storage_hasdocs_test.go`、RAG 检索测试与受影响的 Redis 交互路径。
