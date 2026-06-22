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
	"GopherAI/pkg/logger"
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
func UserDocDir(accountNo string) (string, error) {
	if accountNo == "" || !filepath.IsLocal(accountNo) {
		logger.Warn("UserDocDir invalid account number", "accountNo", accountNo)
		return "", fmt.Errorf("invalid account number: %q", accountNo)
	}
	dir := filepath.Join(uploadRoot, accountNo)
	if err := fileutil.ValidatePath(uploadRoot, dir); err != nil {
		logger.Warn("UserDocDir path escape blocked", "accountNo", accountNo, "err", err)
		return "", fmt.Errorf("invalid account number: %w", err)
	}
	return dir, nil
}

// ResolveUserDocFilename 解析指定账号已上传的首个文档文件名。
//
// 多文档场景下仅返回任意一个文件名，主要用于兼容旧调用；
// RAG 检索已改为按账号聚合，不再依赖单一文件名。
func ResolveUserDocFilename(accountNo string) (string, error) {
	dir, err := UserDocDir(accountNo)
	if err != nil {
		return "", err
	}
	files, err := os.ReadDir(dir)
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

// HasUserDocs 判断指定账号当前是否已存在任意已上传文档。
// 供 RAG 检索前快速判断是否需要走检索增强，避免对无文档账号空检索。
func HasUserDocs(accountNo string) bool {
	dir, err := UserDocDir(accountNo)
	if err != nil {
		return false
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, f := range files {
		if !f.IsDir() {
			return true
		}
	}
	return false
}

// ListUserDocs 列出指定账号当前已存储的文档文件名。
func (s *LocalDocStorage) ListUserDocs(accountNo string) ([]string, error) {
	dir, err := UserDocDir(accountNo)
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
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
	dir, err := UserDocDir(accountNo)
	if err != nil {
		return err
	}
	return fileutil.RemoveAllFilesInDir(dir)
}

// Save 以 storedName 为文件名保存内容到账号目录，返回保存后的本地路径。
func (s *LocalDocStorage) Save(accountNo, storedName string, content io.Reader) (string, error) {
	dir, err := UserDocDir(accountNo)
	if err != nil {
		return "", err
	}
	if storedName == "" || !filepath.IsLocal(storedName) {
		logger.Warn("Save invalid stored name", "accountNo", accountNo, "storedName", storedName)
		return "", fmt.Errorf("invalid stored name: %q", storedName)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Error("Save mkdir failed", "dir", dir, "err", err)
		return "", err
	}
	localPath := filepath.Join(dir, storedName)
	if err := fileutil.ValidatePath(dir, localPath); err != nil {
		logger.Warn("Save path escape blocked", "accountNo", accountNo, "storedName", storedName, "err", err)
		return "", err
	}
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

// RemoveUserDoc 删除指定账号下名为 storedName 的文档文件（按文档删除）。
// 校验 storedName 合法并防止路径逃逸；文件不存在时视为成功（幂等）。
func (s *LocalDocStorage) RemoveUserDoc(accountNo, storedName string) error {
	dir, err := UserDocDir(accountNo)
	if err != nil {
		return err
	}
	if storedName == "" || !filepath.IsLocal(storedName) {
		logger.Warn("RemoveUserDoc invalid stored name", "accountNo", accountNo, "storedName", storedName)
		return fmt.Errorf("invalid stored name: %q", storedName)
	}
	localPath := filepath.Join(dir, storedName)
	if err := fileutil.ValidatePath(dir, localPath); err != nil {
		logger.Warn("RemoveUserDoc path escape blocked", "accountNo", accountNo, "storedName", storedName, "err", err)
		return err
	}
	if err := os.Remove(localPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logger.Error("RemoveUserDoc remove failed", "path", localPath, "err", err)
		return err
	}
	logger.Info("RemoveUserDoc removed", "accountNo", accountNo, "storedName", storedName)
	return nil
}
