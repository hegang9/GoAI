// Package storage 是文档存储适配层：基于本地文件系统实现 domain/storage.DocStorage，
// 并集中承载 uploads/{account_no} 的文件系统约定。
package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	domainstorage "GopherAI/internal/domain/storage"
	"GopherAI/pkg/fileutil"
)

// uploadRoot 用户上传文档根目录，是上传写入与 RAG 检索读取之间的共享约定。
const uploadRoot = "uploads"

// LocalDocStorage 基于本地文件系统实现 domain/storage.DocStorage 端口。
type LocalDocStorage struct{}

// NewLocalDocStorage 创建本地文档存储。
func NewLocalDocStorage() *LocalDocStorage { return &LocalDocStorage{} }

// 编译期断言：LocalDocStorage 必须满足领域端口。
var _ domainstorage.DocStorage = (*LocalDocStorage)(nil)

// UserDocDir 返回指定账号的文档目录（包级可见，供 infrastructure/rag 复用约定）。
func UserDocDir(accountNo string) string {
	return filepath.Join(uploadRoot, accountNo)
}

// ResolveUserDocFilename 解析指定账号已上传的文档文件名。
// 业务约定：每个账号仅保留一个文档。
func ResolveUserDocFilename(accountNo string) (string, error) {
	files, err := os.ReadDir(UserDocDir(accountNo))
	if err != nil || len(files) == 0 {
		return "", fmt.Errorf("no uploaded file found for account %s", accountNo)
	}
	for _, f := range files {
		if !f.IsDir() {
			return f.Name(), nil
		}
	}
	return "", fmt.Errorf("no valid file found for account %s", accountNo)
}

// ListUserDocs 列出指定账号当前已存储的文档文件名。
func (s *LocalDocStorage) ListUserDocs(accountNo string) ([]string, error) {
	files, err := os.ReadDir(UserDocDir(accountNo))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		if !f.IsDir() {
			names = append(names, f.Name())
		}
	}
	return names, nil
}

// ClearUserDocs 清理指定账号已存储的全部文档文件，保留目录。
func (s *LocalDocStorage) ClearUserDocs(accountNo string) error {
	return fileutil.RemoveAllFilesInDir(UserDocDir(accountNo))
}

// Save 以 storedName 为文件名保存内容到账号目录，返回保存后的本地路径。
func (s *LocalDocStorage) Save(accountNo, storedName string, content io.Reader) (string, error) {
	dir := UserDocDir(accountNo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	localPath := filepath.Join(dir, storedName)
	dst, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, content); err != nil {
		return "", err
	}
	return localPath, nil
}

// Remove 删除指定本地路径的文件，用于失败回滚。
func (s *LocalDocStorage) Remove(localPath string) error {
	return os.Remove(localPath)
}
