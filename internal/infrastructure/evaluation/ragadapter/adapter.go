// Package ragadapter 将生产 RAG Engine 适配为离线评测端口。
package ragadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	domaineval "GopherAI/internal/domain/evaluation"
	redisstore "GopherAI/internal/infrastructure/cache/redis"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/pkg/logger"
)

type indexStore interface {
	IndexStats(ctx context.Context, accountNo string) (bool, int, error)
	LoadIndexMetadata(ctx context.Context, accountNo string) ([]byte, bool, error)
	SaveIndexMetadata(ctx context.Context, accountNo string, metadata []byte) error
}

// Adapter 为评测提供文本落盘、索引重置和检索轨迹映射。
type Adapter struct {
	engine      *raginfra.Engine
	vectorStore indexStore
	tmpDir      string
}

// New 创建评测适配器及其隔离临时目录。
func New(engine *raginfra.Engine, vectorStore *redisstore.VectorStore) (*Adapter, error) {
	tmpDir, err := os.MkdirTemp("", "ragbench-*")
	if err != nil {
		return nil, fmt.Errorf("create benchmark temp dir: %w", err)
	}
	return &Adapter{engine: engine, vectorStore: vectorStore, tmpDir: tmpDir}, nil
}

// Close 清理索引期间使用的临时目录。
func (a *Adapter) Close() error {
	return os.RemoveAll(a.tmpDir)
}

// InspectIndex 同时校验 RediSearch 索引和成功建库后写入的 manifest。
func (a *Adapter) InspectIndex(ctx context.Context, accountNo string) (domaineval.IndexState, error) {
	exists, indexedChunks, err := a.vectorStore.IndexStats(ctx, accountNo)
	if err != nil {
		return domaineval.IndexState{}, fmt.Errorf("inspect vector index: %w", err)
	}
	if !exists {
		return domaineval.IndexState{}, nil
	}

	raw, metadataExists, err := a.vectorStore.LoadIndexMetadata(ctx, accountNo)
	if err != nil {
		return domaineval.IndexState{}, fmt.Errorf("load index state: %w", err)
	}
	if !metadataExists {
		return domaineval.IndexState{}, nil
	}
	var state domaineval.IndexState
	if err := json.Unmarshal(raw, &state); err != nil {
		logger.Warn("ragbench index state invalid, rebuild required", "accountNo", accountNo, "err", err)
		return domaineval.IndexState{}, nil
	}
	if state.IndexedChunks <= 0 || state.IndexedChunks != indexedChunks {
		logger.Warn("ragbench indexed chunk count changed, rebuild required",
			"accountNo", accountNo, "manifestChunks", state.IndexedChunks, "actualChunks", indexedChunks)
		return domaineval.IndexState{}, nil
	}
	state.Exists = true
	return state, nil
}

// Reset 清空评测账号的向量索引；底层删除会先使完成标记失效。
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

// SaveIndexState 仅在完整 corpus 全部索引成功后写入可复用标记。
func (a *Adapter) SaveIndexState(ctx context.Context, accountNo string, state domaineval.IndexState) error {
	exists, indexedChunks, err := a.vectorStore.IndexStats(ctx, accountNo)
	if err != nil {
		return fmt.Errorf("inspect completed vector index: %w", err)
	}
	if !exists || indexedChunks <= 0 {
		return fmt.Errorf("completed vector index is missing or empty")
	}
	state.Exists = true
	state.IndexedChunks = indexedChunks
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode index state: %w", err)
	}
	if err := a.vectorStore.SaveIndexMetadata(ctx, accountNo, encoded); err != nil {
		return fmt.Errorf("persist index state: %w", err)
	}
	return nil
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
