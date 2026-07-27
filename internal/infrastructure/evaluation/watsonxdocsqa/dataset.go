// Package watsonxdocsqa 负责加载 watsonxDocsQA 的 Parquet 语料与问答集。
package watsonxdocsqa

import (
	"fmt"
	"path/filepath"
	"strings"

	domaineval "GopherAI/internal/domain/evaluation"

	"github.com/parquet-go/parquet-go"
)

const officialCorpusSize = 1144

var officialQuestionCount = map[string]int{"train": 45, "test": 30}

type corpusRow struct {
	DocID      string `parquet:"doc_id"`
	Title      string `parquet:"title"`
	Document   string `parquet:"document"`
	MDDocument string `parquet:"md_document"`
}

type questionRow struct {
	QuestionID              string `parquet:"question_id"`
	Question                string `parquet:"question"`
	CorrectAnswer           string `parquet:"correct_answer"`
	CorrectAnswerDocumentID string `parquet:"correct_answer_document_ids"`
	GroundTruthContexts     string `parquet:"ground_truths_contexts"`
}

// Load 从 datasetDir 加载公共 corpus 和 train/test 问答 split。
func Load(datasetDir, split string) (domaineval.Dataset, error) {
	split = strings.ToLower(strings.TrimSpace(split))
	if split != "train" && split != "test" {
		return domaineval.Dataset{}, fmt.Errorf("unsupported watsonxDocsQA split %q: use train or test", split)
	}

	corpusPath := filepath.Join(datasetDir, "corpus", "train-00000-of-00001.parquet")
	qaPath := filepath.Join(datasetDir, "question_answers", split+"-00000-of-00001.parquet")
	corpus, err := parquet.ReadFile[corpusRow](corpusPath)
	if err != nil {
		return domaineval.Dataset{}, fmt.Errorf("read watsonxDocsQA corpus %q: %w", corpusPath, err)
	}
	questions, err := parquet.ReadFile[questionRow](qaPath)
	if err != nil {
		return domaineval.Dataset{}, fmt.Errorf("read watsonxDocsQA questions %q: %w", qaPath, err)
	}
	if len(corpus) != officialCorpusSize || len(questions) != officialQuestionCount[split] {
		return domaineval.Dataset{}, fmt.Errorf(
			"incomplete watsonxDocsQA dataset: documents=%d (want %d), %s questions=%d (want %d)",
			len(corpus), officialCorpusSize, split, len(questions), officialQuestionCount[split],
		)
	}

	dataset := domaineval.Dataset{
		Documents: make([]domaineval.Document, 0, len(corpus)),
		Questions: make([]domaineval.Question, 0, len(questions)),
	}
	docIDs := make(map[string]struct{}, len(corpus))
	for i, row := range corpus {
		id := strings.TrimSpace(row.DocID)
		if id == "" {
			return domaineval.Dataset{}, fmt.Errorf("corpus row %d has empty doc_id", i)
		}
		if _, exists := docIDs[id]; exists {
			return domaineval.Dataset{}, fmt.Errorf("corpus contains duplicate doc_id %q", id)
		}
		content := row.MDDocument
		if strings.TrimSpace(content) == "" {
			content = row.Document
		}
		if strings.TrimSpace(content) == "" {
			return domaineval.Dataset{}, fmt.Errorf("corpus document %q has no indexable content", id)
		}
		docIDs[id] = struct{}{}
		dataset.Documents = append(dataset.Documents, domaineval.Document{
			ID: id, StoredName: id + ".md", Title: row.Title, Content: content,
		})
	}

	for i, row := range questions {
		id := strings.TrimSpace(row.QuestionID)
		query := strings.TrimSpace(row.Question)
		correctDocID := strings.TrimSpace(row.CorrectAnswerDocumentID)
		if id == "" || query == "" || correctDocID == "" {
			return domaineval.Dataset{}, fmt.Errorf("question row %d is missing question_id, question, or correct document id", i)
		}
		if _, exists := docIDs[correctDocID]; !exists {
			return domaineval.Dataset{}, fmt.Errorf("question %q references unknown document %q", id, correctDocID)
		}
		dataset.Questions = append(dataset.Questions, domaineval.Question{
			ID:                 id,
			Query:              query,
			CorrectAnswer:      row.CorrectAnswer,
			CorrectDocumentID:  correctDocID,
			CorrectStoredName:  correctDocID + ".md",
			GroundTruthContext: row.GroundTruthContexts,
		})
	}

	if len(dataset.Documents) == 0 || len(dataset.Questions) == 0 {
		return domaineval.Dataset{}, fmt.Errorf("watsonxDocsQA dataset is empty: documents=%d questions=%d", len(dataset.Documents), len(dataset.Questions))
	}
	return dataset, nil
}
