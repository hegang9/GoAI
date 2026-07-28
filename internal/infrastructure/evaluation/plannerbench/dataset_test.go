package plannerbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	domaineval "GopherAI/internal/domain/evaluation"
)

func TestLoadCombinesWatsonAndManualCases(t *testing.T) {
	path := writeManifest(t, strings.Join([]string{
		`{"id":"watson-train-1","split":"train","category":"watsonx_single_turn","source":"watsonxDocsQA","question_id":"train_1","expect":{"need_retrieval":true,"doc_filter":{"storedName":"","headers":""},"query_keywords":[]}}`,
		`{"id":"manual-train-1","split":"train","category":"multiturn_reference","source":"manual","history":[{"role":"user","content":"请介绍年假制度"},{"role":"assistant","content":"好的"}],"last_message":"那要提前几天申请？","expect":{"need_retrieval":true,"doc_filter":{"storedName":"","headers":""},"query_keywords":["年假 申请 提前 天数"]}}`,
		`{"id":"manual-test-1","split":"test","category":"no_retrieval","source":"manual","history":[],"last_message":"讲个笑话","expect":{"need_retrieval":false,"doc_filter":{"storedName":"","headers":""},"query_keywords":[]}}`,
	}, "\n"))

	dataset, err := load("unused", path, "train", fakeWatsonLoader)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if len(dataset.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(dataset.Cases))
	}
	if got := dataset.Cases[0].LastMessage; got != "What is Watsonx?" {
		t.Errorf("watson LastMessage = %q", got)
	}
	if got := dataset.Cases[0].Expect.QueryKeywords; len(got) != 1 || got[0] != "What is Watsonx?" {
		t.Errorf("watson QueryKeywords = %#v", got)
	}
	if !dataset.Cases[1].Expect.NeedRetrieval || dataset.Cases[1].LastMessage != "那要提前几天申请？" {
		t.Errorf("manual case = %+v", dataset.Cases[1])
	}
}

func TestLoadRejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "duplicate id",
			line: `{"id":"duplicate","split":"train","category":"no_retrieval","source":"manual","history":[],"last_message":"你好","expect":{"need_retrieval":false,"doc_filter":{"storedName":"","headers":""},"query_keywords":[]}}` + "\n" +
				`{"id":"duplicate","split":"test","category":"no_retrieval","source":"manual","history":[],"last_message":"再见","expect":{"need_retrieval":false,"doc_filter":{"storedName":"","headers":""},"query_keywords":[]}}`,
		},
		{
			name: "negative has query",
			line: `{"id":"negative-query","split":"train","category":"no_retrieval","source":"manual","history":[],"last_message":"你好","expect":{"need_retrieval":false,"doc_filter":{"storedName":"","headers":""},"query_keywords":["你好"]}}`,
		},
		{
			name: "unknown watson question",
			line: `{"id":"watson-missing","split":"train","category":"watsonx_single_turn","source":"watsonxDocsQA","question_id":"missing","expect":{"need_retrieval":true,"doc_filter":{"storedName":"","headers":""},"query_keywords":[]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load("unused", writeManifest(t, tt.line), "train", fakeWatsonLoader)
			if err == nil {
				t.Fatal("load() error = nil")
			}
		})
	}
}

func TestLoadRejectsInvalidHistoryRole(t *testing.T) {
	path := writeManifest(t, `{"id":"bad-role","split":"train","category":"multiturn_reference","source":"manual","history":[{"role":"system","content":"x"}],"last_message":"问题","expect":{"need_retrieval":true,"doc_filter":{"storedName":"","headers":""},"query_keywords":["问题"]}}`)
	if _, err := load("unused", path, "train", fakeWatsonLoader); err == nil {
		t.Fatal("load() error = nil")
	}
}

func TestOfficialEvalsetHasPlannedCoverage(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..", "testdata", "planbench", "evalset.jsonl")
	entries, err := loadManifest(path)
	if err != nil {
		t.Fatalf("loadManifest(%q) error = %v", path, err)
	}
	if len(entries) != 135 {
		t.Fatalf("entries = %d, want 135", len(entries))
	}
	bySplit := map[string]int{}
	byCategory := map[string]int{}
	positives, negatives, watsonx := 0, 0, 0
	for _, entry := range entries {
		bySplit[entry.Split]++
		byCategory[entry.Category]++
		if *entry.Expect.NeedRetrieval {
			positives++
		} else {
			negatives++
		}
		if entry.Source == SourceWatsonx {
			watsonx++
		}
	}
	if bySplit["train"] != 90 || bySplit["test"] != 45 {
		t.Fatalf("split counts = %#v, want train=90 test=45", bySplit)
	}
	wantCategories := map[string]int{
		CategoryWatsonxSingleTurn: 45,
		CategoryMultiturn:         12,
		CategoryExplicitFilter:    10,
		CategoryNoRetrieval:       38,
		CategoryAmbiguous:         30,
	}
	for category, want := range wantCategories {
		if byCategory[category] != want {
			t.Errorf("category %s = %d, want %d", category, byCategory[category], want)
		}
	}
	if positives != 67 || negatives != 68 || watsonx != 45 {
		t.Fatalf("positive/negative/watsonx = %d/%d/%d, want 67/68/45", positives, negatives, watsonx)
	}
}

func fakeWatsonLoader(_ string, split string) (domaineval.Dataset, error) {
	if split == "train" {
		return domaineval.Dataset{Questions: []domaineval.Question{{ID: "train_1", Query: "What is Watsonx?"}}}, nil
	}
	return domaineval.Dataset{Questions: []domaineval.Question{{ID: "test_1", Query: "What is RAG?"}}}, nil
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evalset.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
