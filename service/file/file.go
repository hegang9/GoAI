package file

import (
	"GopherAI/bo"
	"GopherAI/common/logger"
	"GopherAI/common/rag"
	"GopherAI/config"
	"GopherAI/fileutil"
	"GopherAI/id"
	"context"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// UploadRagFile 将用户上传的 RAG 文档保存到账号编号隔离目录。
func UploadRagFile(accountNo string, file *multipart.FileHeader) (bo.FileBO, error) {
	if err := fileutil.ValidateFile(file); err != nil {
		logger.Warn("File validation failed", "err", err)
		return bo.FileBO{}, err
	}

	// 复用 rag 包集中定义的账号编号文档目录约定，确保上传与检索路径一致。
	userDir := rag.UserDocDir(accountNo)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		logger.Error("Failed to create account directory", "dir", userDir, "err", err)
		return bo.FileBO{}, err
	}

	files, err := os.ReadDir(userDir)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() {
				filename := f.Name()
				if err := rag.DeleteIndex(context.Background(), filename); err != nil {
					logger.Warn("Failed to delete index", "filename", filename, "err", err)
				}
			}
		}
	}

	if err := fileutil.RemoveAllFilesInDir(userDir); err != nil {
		logger.Error("Failed to clean account directory", "dir", userDir, "err", err)
		return bo.FileBO{}, err
	}

	uuid := id.GenerateUUID()
	ext := filepath.Ext(file.Filename)
	filename := uuid + ext
	filePath := filepath.Join(userDir, filename)

	src, err := file.Open()
	if err != nil {
		logger.Error("Failed to open uploaded file", "err", err)
		return bo.FileBO{}, err
	}
	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			logger.Error("Failed to close uploaded file", "err", closeErr)
		}
	}()

	dst, err := os.Create(filePath)
	if err != nil {
		logger.Error("Failed to create destination file", "path", filePath, "err", err)
		return bo.FileBO{}, err
	}
	defer func() {
		if closeErr := dst.Close(); closeErr != nil {
			logger.Error("Failed to close uploaded file", "err", closeErr)
		}
	}()

	if _, err := io.Copy(dst, src); err != nil {
		logger.Error("Failed to copy file content", "err", err)
		return bo.FileBO{}, err
	}

	logger.Info("File uploaded successfully", "path", filePath)

	indexer, err := rag.NewRAGIndexer(filename, config.GetConfig().RagModelConfig.RagEmbeddingModel)
	if err != nil {
		logger.Error("Failed to create RAG indexer", "err", err)
		if removeError := os.Remove(filePath); removeError != nil {
			logger.Error("Failed to remove file", "err", removeError)
		}
		return bo.FileBO{}, err
	}

	if err := indexer.IndexFile(context.Background(), filePath); err != nil {
		logger.Error("Failed to index file", "err", err)
		if removeError := os.Remove(filePath); removeError != nil {
			logger.Error("Failed to remove file", "err", removeError)
		}
		if deleteError := rag.DeleteIndex(context.Background(), filename); deleteError != nil {
			logger.Error("Failed to delete index", "err", deleteError)
		}
		return bo.FileBO{}, err
	}

	logger.Info("File indexed successfully", "filename", filename)
	return bo.FileBO{FilePath: filePath}, nil
}
