package rag

import (
	"fmt"
	"os"
	"path/filepath"
)

// userUploadRoot 表示用户上传文档的根目录。
//
// 这是 service/file（写入文档）与 RAG 检索（读取文档）之间共享的文件系统约定。
// 集中定义在此处，避免该约定散落在多个包中、彼此不一致。
const userUploadRoot = "uploads"

// UserDocDir 返回指定用户的文档存储目录。
func UserDocDir(username string) string {
	return filepath.Join(userUploadRoot, username)
}

// ResolveUserDocFilename 解析指定用户已上传的文档文件名。
//
// 当前业务约定：每个用户仅保留一个文档（上传新文档会清理旧文档）。
// 该函数集中承载“文件系统约定”职责，使检索器（retriever）不再耦合
// 具体的目录结构，只需拿到文档名即可工作。
func ResolveUserDocFilename(username string) (string, error) {
	userDir := UserDocDir(username)
	files, err := os.ReadDir(userDir)
	if err != nil || len(files) == 0 {
		return "", fmt.Errorf("no uploaded file found for user %s", username)
	}

	for _, f := range files {
		if !f.IsDir() {
			return f.Name(), nil
		}
	}
	return "", fmt.Errorf("no valid file found for user %s", username)
}
