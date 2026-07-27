package rag

import "testing"

func TestIndexConfigFingerprintIgnoresRetrievalOnlyChanges(t *testing.T) {
	base := Config{
		EmbeddingModel: "embedding-v1", BaseURL: "https://embedding.example/v1", Dimension: 1024,
		ChunkSize: 512, ChunkOverlap: 64, EnableSemanticChunking: true,
		SemanticPercentile: 95, SemanticBufferSize: 1, EnableHeaderInjection: true,
		TopK: 5, MaxDistance: 0.6, RecallTopK: 20, RerankTopK: 5,
	}
	changed := base
	changed.TopK = 10
	changed.MaxDistance = 0.4
	changed.RecallTopK = 40
	changed.RerankTopK = 8
	changed.RerankEnable = true
	changed.RerankMinScore = 0.3
	changed.ContextWindow = 2

	if got, want := IndexConfigFingerprint(changed), IndexConfigFingerprint(base); got != want {
		t.Fatalf("retrieval-only changes altered index fingerprint: got %s, want %s", got, want)
	}
}

func TestIndexConfigFingerprintChangesForIndexConfiguration(t *testing.T) {
	base := Config{
		EmbeddingModel: "embedding-v1", BaseURL: "https://embedding.example/v1", Dimension: 1024,
		ChunkSize: 512, ChunkOverlap: 64, SemanticPercentile: 95, SemanticBufferSize: 1,
	}
	cases := map[string]func(*Config){
		"embedding model":  func(cfg *Config) { cfg.EmbeddingModel = "embedding-v2" },
		"base URL":         func(cfg *Config) { cfg.BaseURL = "https://other.example/v1" },
		"dimension":        func(cfg *Config) { cfg.Dimension = 2048 },
		"chunk size":       func(cfg *Config) { cfg.ChunkSize = 768 },
		"chunk overlap":    func(cfg *Config) { cfg.ChunkOverlap = 96 },
		"semantic mode":    func(cfg *Config) { cfg.EnableSemanticChunking = true },
		"semantic cutoff":  func(cfg *Config) { cfg.SemanticPercentile = 90 },
		"semantic buffer":  func(cfg *Config) { cfg.SemanticBufferSize = 2 },
		"header injection": func(cfg *Config) { cfg.EnableHeaderInjection = true },
	}
	wantDifferentFrom := IndexConfigFingerprint(base)
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if got := IndexConfigFingerprint(changed); got == wantDifferentFrom {
				t.Fatalf("index configuration change %q did not alter fingerprint", name)
			}
		})
	}
}
