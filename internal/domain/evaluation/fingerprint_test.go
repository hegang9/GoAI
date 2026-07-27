package evaluation_test

import (
	"testing"

	"GopherAI/internal/domain/evaluation"
)

func TestFingerprintDocumentsIgnoresDocumentOrder(t *testing.T) {
	first := []evaluation.Document{
		{ID: "a", StoredName: "a.md", Content: "alpha"},
		{ID: "b", StoredName: "b.md", Content: "beta"},
	}
	second := []evaluation.Document{first[1], first[0]}
	if evaluation.FingerprintDocuments(first) != evaluation.FingerprintDocuments(second) {
		t.Fatal("fingerprint changed when only document order changed")
	}
}

func TestFingerprintDocumentsChangesWithIndexedContent(t *testing.T) {
	first := []evaluation.Document{{ID: "a", StoredName: "a.md", Content: "alpha"}}
	second := []evaluation.Document{{ID: "a", StoredName: "a.md", Content: "changed"}}
	if evaluation.FingerprintDocuments(first) == evaluation.FingerprintDocuments(second) {
		t.Fatal("fingerprint did not change with indexed content")
	}
}
