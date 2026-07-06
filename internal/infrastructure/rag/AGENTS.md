# AGENTS.md

## 模块职责

- 本目录实现文档解析、分块、索引、检索、邻居块扩展、提示词构造和可选精排，是 RAG 能力的核心基础设施层。

## 变更约束

- 继续通过 `Engine` 实现 `domain/rag.Indexer`，不要把 Redis、Eino、HTTP reranker 细节泄漏到领域层或应用层。
- 配置兜底、召回/精排降级和索引期元数据写入逻辑要保持显式，避免因为一个可选能力失败而中断整条链路。
- 文档切分要继续兼顾中文、Markdown 标题、语义切分和邻居块定位；调整 chunk 或 metadata 结构前必须同步检查测试覆盖。
- 任何键名、块编号、`storedName:chunk_N` 约定的变化，都要同步检查 `VectorStore`、删除逻辑和检索扩展逻辑。
- `finalizeChunks()` 产出的连续 `chunk_N` 编号、`source` basename、`chunk` 序号和可选 `headers` 元数据属于稳定契约。
- 检索失败或精排失败时应优雅降级，不要把“拿不到上下文”升级成整条聊天链路失败。

## 验证

- 修改后运行 `go test ./internal/infrastructure/rag -v`。
- 同时运行 `go test ./test/... -v`，重点关注 `test/rag_document_test.go`、`test/rag_parser_test.go`、`test/rag_engine_test.go` 和 `test/rag_reranker_test.go`。
