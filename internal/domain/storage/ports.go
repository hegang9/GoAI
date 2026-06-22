// Package storage 是文档存储领域：定义用户上传文档的存储端口。
//
// 该包只声明契约，不依赖具体文件系统实现；实现位于 infrastructure/storage。
package storage

import "io"

// DocStorage 定义用户上传文档的存储端口。
//
// 业务约定（多文档知识库）：每个账号可保留多个文档，上传新文档为追加，
// 不再清理旧文档；删除以文档为粒度，清空知识库使用 ClearUserDocs。
type DocStorage interface {
	// ListUserDocs 列出指定账号当前已存储的全部文档文件名。
	ListUserDocs(accountNo string) ([]string, error)
	// ClearUserDocs 清理指定账号已存储的全部文档文件（清空知识库）。
	ClearUserDocs(accountNo string) error
	// Save 以 storedName 为文件名保存内容，返回保存后的本地路径。
	Save(accountNo, storedName string, content io.Reader) (localPath string, err error)
	// Remove 删除指定本地路径的文件，用于失败回滚或按文档删除。
	Remove(localPath string) error
	// RemoveUserDoc 删除指定账号下名为 storedName 的文档文件（按文档删除）。
	// 实现需校验 storedName 合法并防止路径逃逸；文件不存在时应视为成功（幂等）。
	RemoveUserDoc(accountNo, storedName string) error
}
