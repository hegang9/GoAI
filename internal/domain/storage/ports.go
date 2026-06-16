// Package storage 是文档存储领域：定义用户上传文档的存储端口。
//
// 该包只声明契约，不依赖具体文件系统实现；实现位于 infrastructure/storage。
package storage

import "io"

// DocStorage 定义用户上传文档的存储端口。
//
// 业务约定：每个账号仅保留一个文档，上传新文档会清理旧文档。
type DocStorage interface {
	// ListUserDocs 列出指定账号当前已存储的文档文件名。
	ListUserDocs(accountNo string) ([]string, error)
	// ClearUserDocs 清理指定账号已存储的全部文档文件。
	ClearUserDocs(accountNo string) error
	// Save 以 storedName 为文件名保存内容，返回保存后的本地路径。
	Save(accountNo, storedName string, content io.Reader) (localPath string, err error)
	// Remove 删除指定本地路径的文件，用于失败回滚。
	Remove(localPath string) error
}
