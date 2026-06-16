// Package file 是文件应用服务：编排 RAG 文档上传——校验、清理旧文档与索引、
// 保存新文档、重建向量索引。
//
// 它依赖文档存储端口（storage.DocStorage）与索引端口（rag.Indexer），由 bootstrap 注入。
package file

import (
	"context"
	"io"
	"path/filepath"

	domainrag "GopherAI/internal/domain/rag"
	domainstorage "GopherAI/internal/domain/storage"
	"GopherAI/pkg/code"
	"GopherAI/pkg/fileutil"
	"GopherAI/pkg/id"
	"GopherAI/pkg/logger"
)

// Service 文件应用服务。
type Service struct {
	storage domainstorage.DocStorage
	indexer domainrag.Indexer
}

// NewService 创建文件应用服务。
func NewService(storage domainstorage.DocStorage, indexer domainrag.Indexer) *Service {
	return &Service{storage: storage, indexer: indexer}
}

// UploadRagFile 保存用户上传的 RAG 文档并重建向量索引，返回保存后的文件路径。
//
// originalName 为上传文件原始名（用于校验扩展名与取后缀），content 为文件内容。
func (s *Service) UploadRagFile(ctx context.Context, accountNo, originalName string, content io.Reader) (string, code.Code) {
	// 校验文件类型。
	if err := fileutil.ValidateDocExt(originalName); err != nil {
		logger.Warn("UploadRagFile validation failed", "err", err)
		return "", code.CodeInvalidParams
	}

	// 删除旧文档对应的向量索引（尽力而为）。
	if oldDocs, err := s.storage.ListUserDocs(accountNo); err == nil {
		for _, name := range oldDocs {
			if err := s.indexer.Delete(ctx, name); err != nil {
				logger.Warn("UploadRagFile delete old index failed", "filename", name, "err", err)
			}
		}
	}

	// 清理旧文档文件。
	if err := s.storage.ClearUserDocs(accountNo); err != nil {
		logger.Error("UploadRagFile clear user docs failed", "accountNo", accountNo, "err", err)
		return "", code.CodeServerBusy
	}

	// 以 uuid + 原始后缀生成隔离文件名并保存。
	storedName := id.GenerateUUID() + filepath.Ext(originalName)
	localPath, err := s.storage.Save(accountNo, storedName, content)
	if err != nil {
		logger.Error("UploadRagFile save failed", "accountNo", accountNo, "err", err)
		return "", code.CodeServerBusy
	}
	logger.Info("UploadRagFile saved", "path", localPath)

	// 建立向量索引；失败时回滚文件与索引。
	if err := s.indexer.Index(ctx, storedName, localPath); err != nil {
		logger.Error("UploadRagFile index failed", "err", err)
		if removeErr := s.storage.Remove(localPath); removeErr != nil {
			logger.Error("UploadRagFile rollback remove failed", "err", removeErr)
		}
		if delErr := s.indexer.Delete(ctx, storedName); delErr != nil {
			logger.Error("UploadRagFile rollback delete index failed", "err", delErr)
		}
		return "", code.CodeServerBusy
	}
	logger.Info("UploadRagFile indexed", "filename", storedName)
	return localPath, code.CodeSuccess
}
