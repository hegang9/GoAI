// Package planneradapter 将生产 Planner 适配为离线评测端口。
package planneradapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"GopherAI/internal/domain/chat"
	domaineval "GopherAI/internal/domain/evaluation"
	"GopherAI/internal/infrastructure/ai"
	"GopherAI/internal/infrastructure/storage"
)

const placeholderName = ".planbench-placeholder"

type plannerClient interface {
	Plan(ctx context.Context, accountNo string, messages []chat.Message, explicitFilter chat.RAGFilter) ai.TurnPlan
}

// Adapter 提供 Planner 评测所需的决策调用与测试账号生命周期管理。
type Adapter struct {
	planner plannerClient
	cleanup func() error
}

// New 创建评测适配器，并为专用账号临时准备占位文档。
func New(planner *ai.Planner, accountNo string) (*Adapter, error) {
	return newAdapter(planner, accountNo)
}

func newAdapter(planner plannerClient, accountNo string) (*Adapter, error) {
	cleanup, err := prepareAccount(accountNo)
	if err != nil {
		return nil, err
	}
	return &Adapter{planner: planner, cleanup: cleanup}, nil
}

// Close 清理评测创建的占位文档；预先存在的空目录会保留。
func (a *Adapter) Close() error {
	if a == nil || a.cleanup == nil {
		return nil
	}
	return a.cleanup()
}

// Plan 调用生产 Planner，并转换为评测领域视图。
func (a *Adapter) Plan(ctx context.Context, accountNo string, messages []domaineval.PlannerMessage) (domaineval.PlannerDecision, error) {
	if err := ctx.Err(); err != nil {
		return domaineval.PlannerDecision{}, err
	}
	chatMessages := make([]chat.Message, 0, len(messages))
	for _, message := range messages {
		chatMessages = append(chatMessages, chat.Message{Content: message.Content, IsUser: message.Role == "user"})
	}
	plan := a.planner.Plan(ctx, accountNo, chatMessages, chat.RAGFilter{})
	return domaineval.PlannerDecision{
		NeedRetrieval:  plan.NeedRetrieval,
		RetrievalQuery: plan.RetrievalQuery,
		DocFilter: domaineval.PlannerDocFilter{
			StoredName: plan.DocFilter.StoredName,
			Headers:    plan.DocFilter.Headers,
		},
		Source:     plan.Source,
		Confidence: plan.Confidence,
	}, nil
}

func prepareAccount(accountNo string) (func() error, error) {
	dir, err := storage.UserDocDir(accountNo)
	if err != nil {
		return nil, fmt.Errorf("resolve benchmark account directory: %w", err)
	}
	_, err = os.Stat(dir)
	dirCreated := os.IsNotExist(err)
	if err != nil && !dirCreated {
		return nil, fmt.Errorf("inspect benchmark account directory: %w", err)
	}
	if !dirCreated {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read benchmark account directory: %w", err)
		}
		if len(entries) > 0 {
			return nil, fmt.Errorf("uploads/%s already exists and non-empty, use a clean accountNo", accountNo)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create benchmark account directory: %w", err)
	}
	placeholder := filepath.Join(dir, placeholderName)
	if err := os.WriteFile(placeholder, []byte("planbench"), 0o644); err != nil {
		return nil, fmt.Errorf("create benchmark placeholder: %w", err)
	}
	return func() error {
		if err := os.Remove(placeholder); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove benchmark placeholder: %w", err)
		}
		if dirCreated {
			if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove benchmark account directory: %w", err)
			}
		}
		return nil
	}, nil
}
