package watsonxdocsqa_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"GopherAI/internal/infrastructure/evaluation/watsonxdocsqa"
	"github.com/parquet-go/parquet-go"
)

type corpusFixture struct {
	DocID      string `parquet:"doc_id"`
	Title      string `parquet:"title"`
	Document   string `parquet:"document"`
	MDDocument string `parquet:"md_document"`
}

type questionFixture struct {
	QuestionID              string `parquet:"question_id"`
	Question                string `parquet:"question"`
	CorrectAnswer           string `parquet:"correct_answer"`
	CorrectAnswerDocumentID string `parquet:"correct_answer_document_ids"`
	GroundTruthContexts     string `parquet:"ground_truths_contexts"`
}

func TestLoadMapsCorpusAndQuestions(t *testing.T) {
	dir := t.TempDir()
	corpusPath := filepath.Join(dir, "corpus", "train-00000-of-00001.parquet")
	qaPath := filepath.Join(dir, "question_answers", "test-00000-of-00001.parquet")
	corpus := make([]corpusFixture, 1144)
	for i := range corpus {
		corpus[i] = corpusFixture{DocID: "DOC" + strconv.Itoa(i+1), Document: "plain"}
	}
	corpus[0] = corpusFixture{DocID: "DOC1", Title: "Example", Document: "plain", MDDocument: "# Example\n\nmarkdown"}
	questions := make([]questionFixture, 30)
	for i := range questions {
		questions[i] = questionFixture{
			QuestionID: "test_" + strconv.Itoa(i+1), Question: "What is the example?", CorrectAnswer: "markdown",
			CorrectAnswerDocumentID: "DOC1", GroundTruthContexts: "Example markdown",
		}
	}
	mustWriteParquet(t, corpusPath, corpus)
	mustWriteParquet(t, qaPath, questions)

	dataset, err := watsonxdocsqa.Load(dir, "test")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := len(dataset.Documents); got != 1144 {
		t.Fatalf("len(Documents) = %d, want 1144", got)
	}
	if got := dataset.Documents[0].StoredName; got != "DOC1.md" {
		t.Errorf("StoredName = %q, want %q", got, "DOC1.md")
	}
	if got := dataset.Documents[0].Content; got != "# Example\n\nmarkdown" {
		t.Errorf("Content = %q, want markdown content", got)
	}
	if got := dataset.Questions[0].CorrectStoredName; got != "DOC1.md" {
		t.Errorf("CorrectStoredName = %q, want %q", got, "DOC1.md")
	}
}

func TestLoadRejectsUnknownSplit(t *testing.T) {
	_, err := watsonxdocsqa.Load(t.TempDir(), "validation")
	if err == nil {
		t.Fatal("Load() error = nil, want unsupported split error")
	}
}

func TestLoadRejectsIncompleteOfficialDataset(t *testing.T) {
	dir := t.TempDir()
	mustWriteParquet(t, filepath.Join(dir, "corpus", "train-00000-of-00001.parquet"), []corpusFixture{{
		DocID: "DOC1", Document: "plain",
	}})
	mustWriteParquet(t, filepath.Join(dir, "question_answers", "test-00000-of-00001.parquet"), []questionFixture{{
		QuestionID: "test_1", Question: "question", CorrectAnswerDocumentID: "DOC1",
	}})

	_, err := watsonxdocsqa.Load(dir, "test")
	if err == nil {
		t.Fatal("Load() error = nil, want incomplete dataset error")
	}
}

func mustWriteParquet[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := parquet.WriteFile(path, rows); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
