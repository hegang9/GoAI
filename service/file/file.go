package file

import (
	"GopherAI/common/logger"
	"GopherAI/common/rag"
	"GopherAI/config"
	"GopherAI/utils"
	"context"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// 上传rag相关文件（这里只允许文本文件）
// 其实可以直接将其向量化进行保存，但这边依旧存储到服务器上以便后续可以在服务器上查看历史RAG文件
func UploadRagFile(username string, file *multipart.FileHeader) (string, error) {
	// 校验文件类型和文件名
	if err := utils.ValidateFile(file); err != nil {
		logger.Warn("File validation failed", "err", err)
		return "", err
	}

	// 创建用户目录
	userDir := filepath.Join("uploads", username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		logger.Error("Failed to create user directory", "dir", userDir, "err", err)
		return "", err
	}

	// 删除用户目录中的所有现有文件及其索引（每个用户只能有一个文件）
	files, err := os.ReadDir(userDir)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() {
				filename := f.Name()
				// 删除该文件对应的 Redis 索引
				if err := rag.DeleteIndex(context.Background(), filename); err != nil {
					logger.Warn("Failed to delete index", "filename", filename, "err", err)
					// 继续执行，不因为索引删除失败而中断文件上传
				}
			}
		}
	}
	// 删除用户目录中的所有文件
	if err := utils.RemoveAllFilesInDir(userDir); err != nil {
		logger.Error("Failed to clean user directory", "dir", userDir, "err", err)
		return "", err
	}

	// 生成UUID作为唯一文件名
	uuid := utils.GenerateUUID()

	ext := filepath.Ext(file.Filename)
	filename := uuid + ext
	filePath := filepath.Join(userDir, filename)

	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		logger.Error("Failed to open uploaded file", "err", err)
		return "", err
	}
	defer src.Close()

	// 创建目标文件
	dst, err := os.Create(filePath)
	if err != nil {
		logger.Error("Failed to create destination file", "path", filePath, "err", err)
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		logger.Error("Failed to copy file content", "err", err)
		return "", err
	}

	logger.Info("File uploaded successfully", "path", filePath)

	// 创建 RAG 索引器并对文件进行向量化
	indexer, err := rag.NewRAGIndexer(filename, config.GetConfig().RagModelConfig.RagEmbeddingModel)
	if err != nil {
		logger.Error("Failed to create RAG indexer", "err", err)
		// 删除已上传的文件
		os.Remove(filePath)
		return "", err
	}

	// 读取文件内容并创建向量索引
	if err := indexer.IndexFile(context.Background(), filePath); err != nil {
		logger.Error("Failed to index file", "err", err)
		// 删除已上传的文件和索引
		os.Remove(filePath)
		rag.DeleteIndex(context.Background(), filename)
		return "", err
	}

	logger.Info("File indexed successfully", "filename", filename)
	return filePath, nil
}
