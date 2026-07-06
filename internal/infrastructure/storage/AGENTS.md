# AGENTS.md

## 模块职责

- 本目录实现本地文档存储，负责账号级目录隔离、文件名解析和路径安全检查。

## 变更约束

- 继续以 `uploads/{account_no}` 组织目录，不要把昵称或邮箱写入路径。
- 任何路径拼接都必须维持 basename 归一化与目录逃逸防护，避免删除/读取越权。
- `HasUserDocs`、`ListUserDocs`、`RemoveUserDoc` 等行为要保持与应用层批量删除语义一致。
- `HasUserDocs(accountNo)` 是普通聊天是否进入 RAG 路径的快速门禁；它应保持低成本、无副作用。
- `ResolveUserDocFilename()` 仅用于兼容旧单文档调用，新增多文档行为不要继续依赖它扩展语义。

## 验证

- 修改后运行 `go test ./test/... -v`，重点关注 `test/storage_test.go`、`test/storage_hasdocs_test.go` 和 `test/file_delete_test.go`。
