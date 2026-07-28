// Package plannerbench 负责加载 Planner 离线评测的双源测试集。
package plannerbench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	domaineval "GopherAI/internal/domain/evaluation"
	"GopherAI/internal/infrastructure/evaluation/watsonxdocsqa"
)

const (
	SourceManual  = "manual"
	SourceWatsonx = "watsonxDocsQA"

	CategoryWatsonxSingleTurn = "watsonx_single_turn"
	CategoryNoRetrieval       = "no_retrieval"
	CategoryAmbiguous         = "ambiguous"
	CategoryMultiturn         = "multiturn_reference"
	CategoryExplicitFilter    = "explicit_filter"
)

type watsonLoader func(datasetDir, split string) (domaineval.Dataset, error)

type manifestCase struct {
	ID          string          `json:"id"`
	Split       string          `json:"split"`
	Category    string          `json:"category"`
	Source      string          `json:"source"`
	QuestionID  string          `json:"question_id"`
	History     []manifestMsg   `json:"history"`
	LastMessage *string         `json:"last_message"`
	Expect      *manifestExpect `json:"expect"`
}

type manifestMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type manifestExpect struct {
	NeedRetrieval *bool              `json:"need_retrieval"`
	DocFilter     *manifestDocFilter `json:"doc_filter"`
	QueryKeywords *[]string          `json:"query_keywords"`
}

type manifestDocFilter struct {
	StoredName *string `json:"storedName"`
	Headers    *string `json:"headers"`
}

// Load 按 split 加载 watsonxDocsQA 引用题目与人工标注用例。
func Load(datasetDir, evalsetPath, split string) (domaineval.PlannerDataset, error) {
	return load(datasetDir, evalsetPath, split, watsonxdocsqa.Load)
}

func load(datasetDir, evalsetPath, split string, loadWatson watsonLoader) (domaineval.PlannerDataset, error) {
	split = strings.ToLower(strings.TrimSpace(split))
	if split != "train" && split != "test" {
		return domaineval.PlannerDataset{}, fmt.Errorf("unsupported planner split %q: use train or test", split)
	}
	entries, err := loadManifest(evalsetPath)
	if err != nil {
		return domaineval.PlannerDataset{}, err
	}
	watson, err := loadWatson(datasetDir, split)
	if err != nil {
		return domaineval.PlannerDataset{}, fmt.Errorf("load watsonxDocsQA %s split: %w", split, err)
	}
	questions := make(map[string]domaineval.Question, len(watson.Questions))
	for _, question := range watson.Questions {
		questions[question.ID] = question
	}

	dataset := domaineval.PlannerDataset{}
	for _, entry := range entries {
		if entry.Split != split {
			continue
		}
		plannerCase, err := resolveCase(entry, questions)
		if err != nil {
			return domaineval.PlannerDataset{}, err
		}
		dataset.Cases = append(dataset.Cases, plannerCase)
	}
	if len(dataset.Cases) == 0 {
		return domaineval.PlannerDataset{}, fmt.Errorf("planner evalset has no %s cases", split)
	}
	return dataset, nil
}

func loadManifest(path string) ([]manifestCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open planner evalset %q: %w", path, err)
	}
	defer f.Close()

	seen := make(map[string]struct{})
	var entries []manifestCase
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry manifestCase
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse planner evalset line %d: %w", lineNo, err)
		}
		if err := validateManifestCase(entry); err != nil {
			return nil, fmt.Errorf("validate planner evalset line %d: %w", lineNo, err)
		}
		if _, exists := seen[entry.ID]; exists {
			return nil, fmt.Errorf("planner evalset has duplicate id %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read planner evalset: %w", err)
	}
	return entries, nil
}

func validateManifestCase(entry manifestCase) error {
	if strings.TrimSpace(entry.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if entry.Split != "train" && entry.Split != "test" {
		return fmt.Errorf("case %q has unsupported split %q", entry.ID, entry.Split)
	}
	if !validCategory(entry.Category) {
		return fmt.Errorf("case %q has unsupported category %q", entry.ID, entry.Category)
	}
	if entry.Expect == nil || entry.Expect.NeedRetrieval == nil || entry.Expect.DocFilter == nil ||
		entry.Expect.DocFilter.StoredName == nil || entry.Expect.DocFilter.Headers == nil || entry.Expect.QueryKeywords == nil {
		return fmt.Errorf("case %q has incomplete expect", entry.ID)
	}
	if entry.Source != SourceManual && entry.Source != SourceWatsonx {
		return fmt.Errorf("case %q has unsupported source %q", entry.ID, entry.Source)
	}
	if entry.Source == SourceWatsonx {
		if entry.Category != CategoryWatsonxSingleTurn || strings.TrimSpace(entry.QuestionID) == "" {
			return fmt.Errorf("watsonx case %q requires watsonx_single_turn category and question_id", entry.ID)
		}
		if entry.LastMessage != nil || len(entry.History) != 0 {
			return fmt.Errorf("watsonx case %q must not define history or last_message", entry.ID)
		}
		if !*entry.Expect.NeedRetrieval || *entry.Expect.DocFilter.StoredName != "" || *entry.Expect.DocFilter.Headers != "" || len(*entry.Expect.QueryKeywords) != 0 {
			return fmt.Errorf("watsonx case %q must expect retrieval with empty filter and keywords", entry.ID)
		}
		return nil
	}

	if strings.TrimSpace(entry.QuestionID) != "" || entry.LastMessage == nil || strings.TrimSpace(*entry.LastMessage) == "" {
		return fmt.Errorf("manual case %q requires last_message and must not define question_id", entry.ID)
	}
	for i, message := range entry.History {
		if (message.Role != "user" && message.Role != "assistant") || strings.TrimSpace(message.Content) == "" {
			return fmt.Errorf("manual case %q has invalid history message %d", entry.ID, i)
		}
	}
	if *entry.Expect.NeedRetrieval {
		if len(*entry.Expect.QueryKeywords) == 0 || hasBlankKeyword(*entry.Expect.QueryKeywords) {
			return fmt.Errorf("retrieval case %q requires non-empty query_keywords", entry.ID)
		}
	} else if *entry.Expect.DocFilter.StoredName != "" || *entry.Expect.DocFilter.Headers != "" || len(*entry.Expect.QueryKeywords) != 0 {
		return fmt.Errorf("non-retrieval case %q must have empty filter and keywords", entry.ID)
	}
	if (entry.Category == CategoryNoRetrieval || entry.Category == CategoryAmbiguous) && *entry.Expect.NeedRetrieval {
		return fmt.Errorf("negative category case %q must not retrieve", entry.ID)
	}
	if (entry.Category == CategoryMultiturn || entry.Category == CategoryExplicitFilter) && !*entry.Expect.NeedRetrieval {
		return fmt.Errorf("retrieval category case %q must retrieve", entry.ID)
	}
	if entry.Category == CategoryMultiturn && len(entry.History) == 0 {
		return fmt.Errorf("multiturn case %q requires history", entry.ID)
	}
	if entry.Category == CategoryExplicitFilter && *entry.Expect.DocFilter.StoredName == "" && *entry.Expect.DocFilter.Headers == "" {
		return fmt.Errorf("explicit filter case %q requires storedName or headers", entry.ID)
	}
	return nil
}

func resolveCase(entry manifestCase, questions map[string]domaineval.Question) (domaineval.PlannerCase, error) {
	plannerCase := domaineval.PlannerCase{ID: entry.ID, Split: entry.Split, Category: entry.Category}
	if entry.Source == SourceWatsonx {
		question, ok := questions[entry.QuestionID]
		if !ok {
			return domaineval.PlannerCase{}, fmt.Errorf("watsonx case %q references unknown question %q in %s split", entry.ID, entry.QuestionID, entry.Split)
		}
		plannerCase.LastMessage = question.Query
		plannerCase.Expect = domaineval.PlannerExpectation{
			NeedRetrieval: true,
			QueryKeywords: []string{question.Query},
		}
		return plannerCase, nil
	}
	plannerCase.LastMessage = strings.TrimSpace(*entry.LastMessage)
	plannerCase.History = make([]domaineval.PlannerMessage, 0, len(entry.History))
	for _, message := range entry.History {
		plannerCase.History = append(plannerCase.History, domaineval.PlannerMessage{Role: message.Role, Content: message.Content})
	}
	plannerCase.Expect = domaineval.PlannerExpectation{
		NeedRetrieval: *entry.Expect.NeedRetrieval,
		DocFilter: domaineval.PlannerDocFilter{
			StoredName: *entry.Expect.DocFilter.StoredName,
			Headers:    *entry.Expect.DocFilter.Headers,
		},
		QueryKeywords: append([]string(nil), (*entry.Expect.QueryKeywords)...),
	}
	return plannerCase, nil
}

func validCategory(category string) bool {
	switch category {
	case CategoryWatsonxSingleTurn, CategoryNoRetrieval, CategoryAmbiguous, CategoryMultiturn, CategoryExplicitFilter:
		return true
	default:
		return false
	}
}

func hasBlankKeyword(keywords []string) bool {
	for _, keyword := range keywords {
		if strings.TrimSpace(keyword) == "" {
			return true
		}
	}
	return false
}
