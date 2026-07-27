package ragbench_test

import (
	"bytes"
	"context"
	"testing"

	"GopherAI/internal/application/evaluation/ragbench"
	domaineval "GopherAI/internal/domain/evaluation"
)

type fakeEngine struct {
	results map[string]domaineval.RetrievalTrace
}

func (f *fakeEngine) Reset(context.Context, string) error                              { return nil }
func (f *fakeEngine) IndexDocument(context.Context, string, domaineval.Document) error { return nil }
func (f *fakeEngine) Retrieve(_ context.Context, _ string, query string) (domaineval.RetrievalTrace, error) {
	return f.results[query], nil
}

func TestRunCalculatesUniqueDocumentMetrics(t *testing.T) {
	dataset := domaineval.Dataset{
		Documents: []domaineval.Document{{ID: "DOC1", StoredName: "DOC1.md", Content: "content"}},
		Questions: []domaineval.Question{
			{ID: "q1", Query: "first", CorrectStoredName: "DOC1.md"},
			{ID: "q2", Query: "second", CorrectStoredName: "DOC1.md"},
		},
	}
	engine := &fakeEngine{results: map[string]domaineval.RetrievalTrace{
		"first": {
			Relevant: []domaineval.Candidate{{StoredName: "DOC1.md"}, {StoredName: "other.md"}},
			Reranked: []domaineval.Candidate{{StoredName: "other.md"}, {StoredName: "DOC1.md"}},
			Final: []domaineval.Candidate{
				{StoredName: "other.md"}, {StoredName: "other.md"}, {StoredName: "DOC1.md", Distance: 0.2},
			},
		},
		"second": {},
	}}

	result, err := ragbench.Run(context.Background(), engine, dataset, ragbench.Options{
		AccountNo: "bench", Split: "test", MinRecall: 0.5, MaxEmptyRate: 0.5,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.DocumentRecall != 0.5 || result.EmptyRate != 0.5 {
		t.Fatalf("recall/empty = %.2f/%.2f, want 0.50/0.50", result.DocumentRecall, result.EmptyRate)
	}
	if result.MRR != 0.25 {
		t.Errorf("MRR = %.2f, want 0.25 from second unique document", result.MRR)
	}
	if !result.Passed {
		t.Error("Passed = false, want true at exact thresholds")
	}
}
