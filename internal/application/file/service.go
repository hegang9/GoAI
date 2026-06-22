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

// maxDeleteBatch 单次删除请求允许的最大文档数量，防止超大批量拖垮服务。
const maxDeleteBatch = 50

// Service 文件应用服务。
type Service struct {
	storage domainstorage.DocStorage
	indexer domainrag.Indexer
}

// NewService 创建文件应用服务。
func NewService(storage domainstorage.DocStorage, indexer domainrag.Indexer) *Service {
	return &Service{storage: storage, indexer: indexer}
}

// UploadRagFile 保存用户上传的 RAG 文档并为其建立向量索引，返回保存后的文件路径。
//
// 多文档知识库语义：本方法为“追加”，不再清理账号下已有文档与索引；
// originalName 为上传文件原始名（用于校验扩展名与取后缀），content 为文件内容。
func (s *Service) UploadRagFile(ctx context.Context, accountNo, originalName string, content io.Reader) (string, code.Code) {
	// 校验文件类型。
	if err := fileutil.ValidateDocExt(originalName); err != nil {
		logger.Warn("UploadRagFile validation failed", "err", err)
		return "", code.CodeInvalidParams
	}

	// 以 uuid + 原始后缀生成隔离文件名并保存（追加，不清理旧文档）。
	storedName := id.GenerateUUID() + filepath.Ext(originalName)
	localPath, err := s.storage.Save(accountNo, storedName, content)
	if err != nil {
		logger.Error("UploadRagFile save failed", "accountNo", accountNo, "err", err)
		return "", code.CodeServerBusy
	}
	logger.Info("UploadRagFile saved", "path", localPath)

	// 为该文档建立向量索引；失败时回滚文件与该文档向量。
	if err := s.indexer.Index(ctx, accountNo, storedName, localPath); err != nil {
		logger.Error("UploadRagFile index failed", "err", err)
		if removeErr := s.storage.Remove(localPath); removeErr != nil {
			logger.Error("UploadRagFile rollback remove failed", "err", removeErr)
		}
		if delErr := s.indexer.Delete(ctx, accountNo, storedName); delErr != nil {
			logger.Error("UploadRagFile rollback delete index failed", "err", delErr)
		}
		return "", code.CodeServerBusy
	}
	logger.Info("UploadRagFile indexed", "accountNo", accountNo, "filename", storedName)
	return localPath, code.CodeSuccess
}

// ListRagFiles 列出指定账号当前已上传的全部 RAG 文档存储文件名。
//
// 返回的文件名即上传响应 file_path 的 basename，可直接用于 DeleteRagFiles。
func (s *Service) ListRagFiles(accountNo string) ([]string, code.Code) {
	names, err := s.storage.ListUserDocs(accountNo)
	if err != nil {
		logger.Error("ListRagFiles failed", "accountNo", accountNo, "err", err)
		return nil, code.CodeServerBusy
	}
	logger.Info("ListRagFiles done", "accountNo", accountNo, "count", len(names))
	return names, code.CodeSuccess
}

// DeleteRagFiles 批量删除指定账号下的若干 RAG 文档及其向量数据，返回成功删除的文件名列表。
//
// 入参 filenames 为文档存储文件名（上传响应 file_path 的 basename）；
// 采用尽力而为策略：单个文档删除失败不影响其余文档，仅记录日志；
// 当全部删除失败时返回 CodeServerBusy，否则返回成功（含部分成功）。
func (s *Service) DeleteRagFiles(ctx context.Context, accountNo string, filenames []string) ([]string, code.Code) {
	if len(filenames) == 0 || len(filenames) > maxDeleteBatch {
		logger.Warn("DeleteRagFiles invalid filenames count", "accountNo", accountNo, "count", len(filenames))
		return nil, code.CodeInvalidParams
	}

	deleted := make([]string, 0, len(filenames))
	var failed int
	// 去重并归一化为 basename，避免重复删除与路径分隔符。
	seen := make(map[string]struct{}, len(filenames))
	for _, raw := range filenames {
		storedName := filepath.Base(raw)
		if storedName == "" || storedName == "." || storedName == string(filepath.Separator) {
			failed++
			logger.Warn("DeleteRagFiles skip invalid filename", "accountNo", accountNo, "filename", raw)
			continue
		}
		if _, ok := seen[storedName]; ok {
			continue
		}
		seen[storedName] = struct{}{}

		// 先删向量数据，再删文件；任一失败计为失败但不中断其余删除。
		ok := true
		if err := s.indexer.Delete(ctx, accountNo, storedName); err != nil {
			ok = false
			logger.Error("DeleteRagFiles delete index failed", "accountNo", accountNo, "filename", storedName, "err", err)
		}
		if err := s.storage.RemoveUserDoc(accountNo, storedName); err != nil {
			ok = false
			logger.Error("DeleteRagFiles remove file failed", "accountNo", accountNo, "filename", storedName, "err", err)
		}
		if ok {
			deleted = append(deleted, storedName)
		} else {
			failed++
		}
	}

	// 全部失败才视为服务端错误；部分成功仍返回成功并附已删除列表。
	if len(deleted) == 0 && failed > 0 {
		return nil, code.CodeServerBusy
	}
	logger.Info("DeleteRagFiles done", "accountNo", accountNo, "deleted", len(deleted), "failed", failed)
	return deleted, code.CodeSuccess
}
