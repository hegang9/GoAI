package file

import (
	"GopherAI/bo"
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

func UploadRagFile(username string, file *multipart.FileHeader) (bo.FileBO, error) {
	if err := utils.ValidateFile(file); err != nil {
		logger.Warn("File validation failed", "err", err)
		return bo.FileBO{}, err
	}

	userDir := filepath.Join("uploads", username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		logger.Error("Failed to create user directory", "dir", userDir, "err", err)
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

	if err := utils.RemoveAllFilesInDir(userDir); err != nil {
		logger.Error("Failed to clean user directory", "dir", userDir, "err", err)
		return bo.FileBO{}, err
	}

	uuid := utils.GenerateUUID()
	ext := filepath.Ext(file.Filename)
	filename := uuid + ext
	filePath := filepath.Join(userDir, filename)

	src, err := file.Open()
	if err != nil {
		logger.Error("Failed to open uploaded file", "err", err)
		return bo.FileBO{}, err
	}
	defer src.Close()

	dst, err := os.Create(filePath)
	if err != nil {
		logger.Error("Failed to create destination file", "path", filePath, "err", err)
		return bo.FileBO{}, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		logger.Error("Failed to copy file content", "err", err)
		return bo.FileBO{}, err
	}

	logger.Info("File uploaded successfully", "path", filePath)

	indexer, err := rag.NewRAGIndexer(filename, config.GetConfig().RagModelConfig.RagEmbeddingModel)
	if err != nil {
		logger.Error("Failed to create RAG indexer", "err", err)
		os.Remove(filePath)
		return bo.FileBO{}, err
	}

	if err := indexer.IndexFile(context.Background(), filePath); err != nil {
		logger.Error("Failed to index file", "err", err)
		os.Remove(filePath)
		rag.DeleteIndex(context.Background(), filename)
		return bo.FileBO{}, err
	}

	logger.Info("File indexed successfully", "filename", filename)
	return bo.FileBO{FilePath: filePath}, nil
}
