package planneradapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"GopherAI/internal/domain/chat"
	domaineval "GopherAI/internal/domain/evaluation"
	"GopherAI/internal/infrastructure/ai"
)

type fakePlanner struct {
	plan ai.TurnPlan
}

func (f fakePlanner) Plan(_ context.Context, _ string, messages []chat.Message, _ chat.RAGFilter) ai.TurnPlan {
	return f.plan
}

func TestAdapterPreparesAndCleansDedicatedAccount(t *testing.T) {
	accountNo := "planbench_adapter_test_account"
	dir := filepath.Join("uploads", accountNo)
	_ = os.RemoveAll(dir)
	adapter, err := newAdapter(fakePlanner{}, accountNo)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, placeholderName)); err != nil {
		t.Fatalf("placeholder stat error = %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory remains after cleanup: %v", err)
	}
}

func TestAdapterRejectsNonemptyAccountAndMapsDecision(t *testing.T) {
	accountNo := "planbench_adapter_nonempty"
	dir := filepath.Join("uploads", accountNo)
	defer os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.md"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newAdapter(fakePlanner{}, accountNo); err == nil {
		t.Fatal("newAdapter() error = nil")
	}

	cleanAccount := "planbench_adapter_mapping"
	cleanDir := filepath.Join("uploads", cleanAccount)
	defer os.RemoveAll(cleanDir)
	adapter, err := newAdapter(fakePlanner{plan: ai.TurnPlan{
		NeedRetrieval: true, RetrievalQuery: "年假申请", DocFilter: chat.RAGFilter{StoredName: "员工手册.md", Headers: "年假"}, Source: "planner", Confidence: "high",
	}}, cleanAccount)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	decision, err := adapter.Plan(context.Background(), cleanAccount, []domaineval.PlannerMessage{{Role: "user", Content: "年假怎么申请"}})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.NeedRetrieval || decision.DocFilter.StoredName != "员工手册.md" || decision.Confidence != "high" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestAdapterRejectsInvalidAccountAndCanceledContext(t *testing.T) {
	if _, err := newAdapter(fakePlanner{}, "../planbench-invalid-account"); err == nil {
		t.Fatal("newAdapter() error = nil for invalid account")
	}

	accountNo := "planbench_adapter_canceled"
	dir := filepath.Join("uploads", accountNo)
	defer os.RemoveAll(dir)
	adapter, err := newAdapter(fakePlanner{}, accountNo)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Plan(ctx, accountNo, []domaineval.PlannerMessage{{Role: "user", Content: "测试"}}); err == nil {
		t.Fatal("Plan() error = nil for canceled context")
	}
}
