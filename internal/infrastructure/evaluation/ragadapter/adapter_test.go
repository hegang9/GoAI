package ragadapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domaineval "GopherAI/internal/domain/evaluation"
)

type fakeIndexStore struct {
	exists         bool
	indexedChunks  int
	statsErr       error
	metadata       []byte
	metadataExists bool
	metadataErr    error
	saved          []byte
	saveErr        error
}

func (f *fakeIndexStore) IndexStats(context.Context, string) (bool, int, error) {
	return f.exists, f.indexedChunks, f.statsErr
}

func (f *fakeIndexStore) LoadIndexMetadata(context.Context, string) ([]byte, bool, error) {
	return f.metadata, f.metadataExists, f.metadataErr
}

func (f *fakeIndexStore) SaveIndexMetadata(_ context.Context, _ string, metadata []byte) error {
	f.saved = append([]byte(nil), metadata...)
	return f.saveErr
}

func TestInspectIndexRequiresIndexAndMetadata(t *testing.T) {
	tests := []struct {
		name  string
		store *fakeIndexStore
	}{
		{name: "index missing", store: &fakeIndexStore{}},
		{name: "metadata missing", store: &fakeIndexStore{exists: true, indexedChunks: 10}},
		{name: "metadata corrupt", store: &fakeIndexStore{exists: true, indexedChunks: 10, metadataExists: true, metadata: []byte("{")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &Adapter{vectorStore: tt.store}
			state, err := adapter.InspectIndex(context.Background(), "bench")
			if err != nil {
				t.Fatalf("InspectIndex() error = %v", err)
			}
			if state.Exists {
				t.Fatal("InspectIndex() returned reusable state")
			}
		})
	}
}

func TestInspectIndexRejectsChangedChunkCount(t *testing.T) {
	metadata, err := json.Marshal(domaineval.IndexState{
		CorpusFingerprint: "corpus", IndexConfigFingerprint: "config", IndexedChunks: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &Adapter{vectorStore: &fakeIndexStore{
		exists: true, indexedChunks: 10, metadataExists: true, metadata: metadata,
	}}
	state, err := adapter.InspectIndex(context.Background(), "bench")
	if err != nil {
		t.Fatalf("InspectIndex() error = %v", err)
	}
	if state.Exists {
		t.Fatal("InspectIndex() reused index with changed chunk count")
	}
}

func TestInspectIndexReturnsCompatibleState(t *testing.T) {
	metadata, err := json.Marshal(domaineval.IndexState{
		CorpusFingerprint: "corpus", IndexConfigFingerprint: "config", IndexedChunks: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &Adapter{vectorStore: &fakeIndexStore{
		exists: true, indexedChunks: 10, metadataExists: true, metadata: metadata,
	}}
	state, err := adapter.InspectIndex(context.Background(), "bench")
	if err != nil {
		t.Fatalf("InspectIndex() error = %v", err)
	}
	if !state.Exists || state.CorpusFingerprint != "corpus" || state.IndexConfigFingerprint != "config" {
		t.Fatalf("InspectIndex() state = %+v", state)
	}
}

func TestSaveIndexStatePersistsActualChunkCount(t *testing.T) {
	store := &fakeIndexStore{exists: true, indexedChunks: 10}
	adapter := &Adapter{vectorStore: store}
	err := adapter.SaveIndexState(context.Background(), "bench", domaineval.IndexState{
		CorpusFingerprint: "corpus", IndexConfigFingerprint: "config",
	})
	if err != nil {
		t.Fatalf("SaveIndexState() error = %v", err)
	}
	var saved domaineval.IndexState
	if err := json.Unmarshal(store.saved, &saved); err != nil {
		t.Fatalf("decode saved state: %v", err)
	}
	if !saved.Exists || saved.IndexedChunks != 10 {
		t.Fatalf("saved state = %+v", saved)
	}
}

func TestInspectIndexReturnsStorageErrors(t *testing.T) {
	adapter := &Adapter{vectorStore: &fakeIndexStore{statsErr: errors.New("redis unavailable")}}
	if _, err := adapter.InspectIndex(context.Background(), "bench"); err == nil {
		t.Fatal("InspectIndex() error = nil")
	}
}
