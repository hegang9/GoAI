package ragbench_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"GopherAI/internal/application/evaluation/ragbench"
	domaineval "GopherAI/internal/domain/evaluation"
)

type fakeEngine struct {
	results      map[string]domaineval.RetrievalTrace
	state        domaineval.IndexState
	inspectErr   error
	indexErrAt   int
	saveErr      error
	inspectCount int
	resetCount   int
	indexedCount int
	saveCount    int
	savedState   domaineval.IndexState
}

func (f *fakeEngine) InspectIndex(context.Context, string) (domaineval.IndexState, error) {
	f.inspectCount++
	return f.state, f.inspectErr
}
func (f *fakeEngine) Reset(context.Context, string) error { f.resetCount++; return nil }
func (f *fakeEngine) IndexDocument(context.Context, string, domaineval.Document) error {
	f.indexedCount++
	if f.indexErrAt > 0 && f.indexedCount == f.indexErrAt {
		return errors.New("index failed")
	}
	return nil
}
func (f *fakeEngine) SaveIndexState(_ context.Context, _ string, state domaineval.IndexState) error {
	f.saveCount++
	f.savedState = state
	return f.saveErr
}

func benchmarkDataset() domaineval.Dataset {
	return domaineval.Dataset{
		CorpusFingerprint: "corpus-v2",
		Documents: []domaineval.Document{
			{ID: "DOC1", StoredName: "DOC1.md", Content: "content"},
			{ID: "DOC2", StoredName: "DOC2.md", Content: "content"},
		},
		Questions: []domaineval.Question{{ID: "q1", Query: "query", CorrectStoredName: "DOC1.md"}},
	}
}

func benchmarkOptions() ragbench.Options {
	return ragbench.Options{
		AccountNo: "bench", Split: "test", IndexConfigFingerprint: "index-v2",
		MinRecall: 0, MaxEmptyRate: 1,
	}
}
func (f *fakeEngine) Retrieve(_ context.Context, _ string, query string) (domaineval.RetrievalTrace, error) {
	return f.results[query], nil
}

func TestRunCalculatesUniqueDocumentMetrics(t *testing.T) {
	dataset := domaineval.Dataset{
		CorpusFingerprint: "corpus-v1",
		Documents:         []domaineval.Document{{ID: "DOC1", StoredName: "DOC1.md", Content: "content"}},
		Questions: []domaineval.Question{
			{ID: "q1", Query: "first", CorrectStoredName: "DOC1.md"},
			{ID: "q2", Query: "second", CorrectStoredName: "DOC1.md"},
		},
	}
	engine := &fakeEngine{state: domaineval.IndexState{
		Exists: true, CorpusFingerprint: "corpus-v1", IndexConfigFingerprint: "index-v1",
	}, results: map[string]domaineval.RetrievalTrace{
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
		AccountNo: "bench", Split: "test", IndexConfigFingerprint: "index-v1",
		MinRecall: 0.5, MaxEmptyRate: 0.5,
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
	if engine.resetCount != 0 || engine.indexedCount != 0 {
		t.Errorf("compatible index was rebuilt: reset=%d indexed=%d", engine.resetCount, engine.indexedCount)
	}
}

func TestRunRebuildsWhenIndexConfigurationChanges(t *testing.T) {
	dataset := domaineval.Dataset{
		CorpusFingerprint: "corpus-v1",
		Documents:         []domaineval.Document{{ID: "DOC1", StoredName: "DOC1.md", Content: "content"}},
		Questions:         []domaineval.Question{{ID: "q1", Query: "query", CorrectStoredName: "DOC1.md"}},
	}
	engine := &fakeEngine{
		state:   domaineval.IndexState{Exists: true, CorpusFingerprint: "corpus-v1", IndexConfigFingerprint: "index-v1"},
		results: map[string]domaineval.RetrievalTrace{"query": {Final: []domaineval.Candidate{{StoredName: "DOC1.md"}}}},
	}

	_, err := ragbench.Run(context.Background(), engine, dataset, ragbench.Options{
		AccountNo: "bench", Split: "test", IndexConfigFingerprint: "index-v2",
		MinRecall: 0, MaxEmptyRate: 1,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if engine.resetCount != 1 || engine.indexedCount != 1 {
		t.Fatalf("rebuild calls = reset %d/index %d, want 1/1", engine.resetCount, engine.indexedCount)
	}
	if engine.savedState.IndexConfigFingerprint != "index-v2" {
		t.Errorf("saved index fingerprint = %q, want index-v2", engine.savedState.IndexConfigFingerprint)
	}
}

func TestRunRebuildsWhenCorpusChanges(t *testing.T) {
	engine := &fakeEngine{
		state:   domaineval.IndexState{Exists: true, CorpusFingerprint: "corpus-v1", IndexConfigFingerprint: "index-v2"},
		results: map[string]domaineval.RetrievalTrace{"query": {}},
	}
	if _, err := ragbench.Run(context.Background(), engine, benchmarkDataset(), benchmarkOptions(), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if engine.resetCount != 1 || engine.indexedCount != 2 || engine.saveCount != 1 {
		t.Fatalf("rebuild calls = reset %d/index %d/save %d, want 1/2/1", engine.resetCount, engine.indexedCount, engine.saveCount)
	}
}

func TestRunForceReindexBypassesInspection(t *testing.T) {
	engine := &fakeEngine{
		inspectErr: errors.New("broken manifest"),
		results:    map[string]domaineval.RetrievalTrace{"query": {}},
	}
	opts := benchmarkOptions()
	opts.ForceReindex = true
	if _, err := ragbench.Run(context.Background(), engine, benchmarkDataset(), opts, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if engine.inspectCount != 0 || engine.resetCount != 1 {
		t.Fatalf("inspect/reset = %d/%d, want 0/1", engine.inspectCount, engine.resetCount)
	}
}

func TestRunDoesNotSaveStateAfterInterruptedIndexing(t *testing.T) {
	engine := &fakeEngine{indexErrAt: 2, results: map[string]domaineval.RetrievalTrace{"query": {}}}
	_, err := ragbench.Run(context.Background(), engine, benchmarkDataset(), benchmarkOptions(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("Run() error = nil, want indexing error")
	}
	if engine.saveCount != 0 {
		t.Fatalf("saveCount = %d, want 0", engine.saveCount)
	}
}

func TestRunReturnsStateSaveFailure(t *testing.T) {
	engine := &fakeEngine{saveErr: errors.New("save failed"), results: map[string]domaineval.RetrievalTrace{"query": {}}}
	_, err := ragbench.Run(context.Background(), engine, benchmarkDataset(), benchmarkOptions(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("Run() error = nil, want save error")
	}
	if engine.saveCount != 1 {
		t.Fatalf("saveCount = %d, want 1", engine.saveCount)
	}
}
