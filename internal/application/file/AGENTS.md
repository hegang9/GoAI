# AGENTS.md

## 模块职责

- 本目录负责 RAG 文档的上传、列出和删除编排：文件校验、落盘、索引建立、索引/文件回滚与批量删除。

## 变更约束

- 继续保持“追加式知识库”语义，除非需求明确要求，否则不要在上传时清空用户历史文档。
- 索引失败要维持回滚路径：删文件、删该文档向量，避免磁盘和索引状态不一致。
- 删除逻辑继续使用 `basename` 归一化和去重，防止路径注入与重复删除。
- 这一层只编排 `storage.DocStorage` 与 `rag.Indexer`，不要把 Redis、文件系统或 HTTP 细节直接写进服务代码。

## 验证

- 修改后运行 `go test ./test/... -v`，重点关注 `test/file_delete_test.go`、`test/storage_test.go`、`test/storage_hasdocs_test.go` 和 RAG 相关测试。
