package planbench

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	domaineval "GopherAI/internal/domain/evaluation"
)

type fakePlanner struct {
	decisions map[string]domaineval.PlannerDecision
	err       error
}

func (f fakePlanner) Plan(_ context.Context, _ string, messages []domaineval.PlannerMessage) (domaineval.PlannerDecision, error) {
	if f.err != nil {
		return domaineval.PlannerDecision{}, f.err
	}
	return f.decisions[messages[len(messages)-1].Content], nil
}

func TestRunCalculatesObservationMetricsByCategory(t *testing.T) {
	dataset := domaineval.PlannerDataset{Cases: []domaineval.PlannerCase{
		{ID: "positive", Split: "test", Category: "multiturn_reference", LastMessage: "那要提前几天？", Expect: domaineval.PlannerExpectation{
			NeedRetrieval: true, QueryKeywords: []string{"年假 申请 提前 天数"},
		}},
		{ID: "negative", Split: "test", Category: "no_retrieval", LastMessage: "讲个笑话", Expect: domaineval.PlannerExpectation{}},
	}}
	planner := fakePlanner{decisions: map[string]domaineval.PlannerDecision{
		"那要提前几天？": {NeedRetrieval: true, RetrievalQuery: "年假申请需要提前几天", Source: "planner", Confidence: "high"},
		"讲个笑话":    {NeedRetrieval: true, Source: "planner", Confidence: "high"},
	}}
	output := &bytes.Buffer{}
	result, err := Run(context.Background(), planner, dataset, Options{AccountNo: "bench", Split: "test"}, output)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Cases != 2 || result.MisjudgeRate != 0.5 || result.FilterAccuracy != 1 || result.RougeCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Categories["no_retrieval"].MisjudgeRate != 1 || result.Categories["multiturn_reference"].MisjudgeRate != 0 {
		t.Fatalf("categories = %+v", result.Categories)
	}
	if !strings.Contains(output.String(), "RESULT: COMPLETE") {
		t.Fatalf("output = %s", output.String())
	}
}

func TestRunHonorsLimitAndPropagatesPlannerError(t *testing.T) {
	dataset := domaineval.PlannerDataset{Cases: []domaineval.PlannerCase{
		{ID: "one", Category: "no_retrieval", LastMessage: "one"},
		{ID: "two", Category: "no_retrieval", LastMessage: "two"},
	}}
	result, err := Run(context.Background(), fakePlanner{decisions: map[string]domaineval.PlannerDecision{}}, dataset, Options{AccountNo: "bench", Limit: 1}, &bytes.Buffer{})
	if err != nil || result.Cases != 1 {
		t.Fatalf("Run() result/error = %+v/%v", result, err)
	}
	_, err = Run(context.Background(), fakePlanner{err: errors.New("planner failed")}, dataset, Options{AccountNo: "bench"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "planner failed") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRougeLUsesWhitespaceBetweenEnglishKeywords(t *testing.T) {
	if got := rougeL("foundation models", "foundation models"); got != 1 {
		t.Fatalf("ROUGE-L = %v, want 1", got)
	}
}
