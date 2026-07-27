// Package ragadapter 将生产 RAG Engine 适配为离线评测端口。
package ragadapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	domaineval "GopherAI/internal/domain/evaluation"
	raginfra "GopherAI/internal/infrastructure/rag"
)

// Adapter 为评测提供文本落盘、索引重置和检索轨迹映射。
type Adapter struct {
	engine *raginfra.Engine
	tmpDir string
}

// New 创建评测适配器及其隔离临时目录。
func New(engine *raginfra.Engine) (*Adapter, error) {
	tmpDir, err := os.MkdirTemp("", "ragbench-*")
	if err != nil {
		return nil, fmt.Errorf("create benchmark temp dir: %w", err)
	}
	return &Adapter{engine: engine, tmpDir: tmpDir}, nil
}

// Close 清理索引期间使用的临时目录。
func (a *Adapter) Close() error {
	return os.RemoveAll(a.tmpDir)
}

// Reset 清空评测账号的向量索引。
func (a *Adapter) Reset(ctx context.Context, accountNo string) error {
	return a.engine.DeleteAll(ctx, accountNo)
}

// IndexDocument 将内存中的 Markdown 文档临时落盘并交给生产索引器。
func (a *Adapter) IndexDocument(ctx context.Context, accountNo string, document domaineval.Document) error {
	if filepath.Base(document.StoredName) != document.StoredName {
		return fmt.Errorf("invalid benchmark storedName %q", document.StoredName)
	}
	path := filepath.Join(a.tmpDir, document.StoredName)
	if err := os.WriteFile(path, []byte(document.Content), 0o644); err != nil {
		return err
	}
	defer os.Remove(path)
	return a.engine.Index(ctx, accountNo, document.StoredName, path)
}

// Retrieve 执行生产检索，并只暴露评测所需字段。
func (a *Adapter) Retrieve(ctx context.Context, accountNo, query string) (domaineval.RetrievalTrace, error) {
	detail, err := a.engine.RetrieveDetail(ctx, accountNo, query, raginfra.RetrieveFilter{})
	if err != nil {
		return domaineval.RetrievalTrace{}, err
	}
	return domaineval.RetrievalTrace{
		Relevant: toCandidates(detail.Relevant),
		Reranked: toCandidates(detail.Reranked),
		Final:    toCandidates(detail.Final),
	}, nil
}

func toCandidates(documents []raginfra.DocScore) []domaineval.Candidate {
	candidates := make([]domaineval.Candidate, 0, len(documents))
	for _, document := range documents {
		candidates = append(candidates, domaineval.Candidate{
			StoredName: document.StoredName,
			Distance:   document.Distance,
		})
	}
	return candidates
}
