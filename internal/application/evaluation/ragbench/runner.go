// Package ragbench 编排公共语料索引与金标准文档召回评测。
package ragbench

import (
	"context"
	"fmt"
	"io"

	domaineval "GopherAI/internal/domain/evaluation"
)

// Engine 定义评测用例所需的索引与可观测检索能力。
type Engine interface {
	Reset(ctx context.Context, accountNo string) error
	IndexDocument(ctx context.Context, accountNo string, document domaineval.Document) error
	Retrieve(ctx context.Context, accountNo, query string) (domaineval.RetrievalTrace, error)
}

// Options 控制索引生命周期、样本规模和质量门禁。
type Options struct {
	AccountNo    string
	Split        string
	Reindex      bool
	Limit        int
	MinRecall    float64
	MaxEmptyRate float64
}

// Result 汇总一次 watsonxDocsQA 召回评测。
type Result struct {
	Cases           int
	Documents       int
	DocumentRecall  float64
	EmptyRate       float64
	MRR             float64
	RerankGain      float64
	RerankEligible  int
	AverageDistance float64
	DistanceCount   int
	Passed          bool
}

// Run 对同一份公共语料只建一次索引，再执行指定问题集。
func Run(ctx context.Context, engine Engine, dataset domaineval.Dataset, opts Options, output io.Writer) (Result, error) {
	if opts.AccountNo == "" {
		return Result{}, fmt.Errorf("accountNo cannot be empty")
	}
	if opts.Limit < 0 {
		return Result{}, fmt.Errorf("limit must be >= 0")
	}
	if opts.MinRecall < 0 || opts.MinRecall > 1 || opts.MaxEmptyRate < 0 || opts.MaxEmptyRate > 1 {
		return Result{}, fmt.Errorf("quality thresholds must be between 0 and 1")
	}
	questions := dataset.Questions
	if opts.Limit > 0 && opts.Limit < len(questions) {
		questions = questions[:opts.Limit]
	}
	if len(dataset.Documents) == 0 || len(questions) == 0 {
		return Result{}, fmt.Errorf("dataset is empty: documents=%d questions=%d", len(dataset.Documents), len(questions))
	}

	if opts.Reindex {
		fmt.Fprintf(output, "rebuilding index account=%s documents=%d\n", opts.AccountNo, len(dataset.Documents))
		if err := engine.Reset(ctx, opts.AccountNo); err != nil {
			return Result{}, fmt.Errorf("clear account %s index: %w", opts.AccountNo, err)
		}
		for i, document := range dataset.Documents {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			if err := engine.IndexDocument(ctx, opts.AccountNo, document); err != nil {
				return Result{}, fmt.Errorf("index corpus document %s (%d/%d): %w", document.ID, i+1, len(dataset.Documents), err)
			}
			if (i+1)%50 == 0 || i+1 == len(dataset.Documents) {
				fmt.Fprintf(output, "indexed %d/%d documents\n", i+1, len(dataset.Documents))
			}
		}
	} else {
		fmt.Fprintf(output, "reuse existing index account=%s (caller must ensure it is complete and configuration-compatible)\n", opts.AccountNo)
	}

	var hitCount, emptyCount, rerankGain, rerankEligible int
	var reciprocalRankSum, distanceSum float64
	var distanceCount int
	for i, question := range questions {
		trace, err := engine.Retrieve(ctx, opts.AccountNo, question.Query)
		if err != nil {
			return Result{}, fmt.Errorf("retrieve question %s: %w", question.ID, err)
		}
		hitRank := uniqueDocumentRank(trace.Final, question.CorrectStoredName)
		if hitRank >= 0 {
			hitCount++
			reciprocalRankSum += 1 / float64(hitRank+1)
		}
		if len(trace.Final) == 0 {
			emptyCount++
		}

		rankRelevant := uniqueDocumentRank(trace.Relevant, question.CorrectStoredName)
		rankReranked := uniqueDocumentRank(trace.Reranked, question.CorrectStoredName)
		if rankRelevant >= 0 && rankReranked >= 0 {
			rerankEligible++
			if rankReranked < rankRelevant {
				rerankGain++
			}
		}
		for _, candidate := range trace.Final {
			if candidate.Distance >= 0 {
				distanceSum += candidate.Distance
				distanceCount++
			}
		}
		fmt.Fprintf(output, "[%d] id=%s final_chunks=%d gold=%s hit=%v(document_rank=%d) rerank(rel=%d,rr=%d) query=%q\n",
			i, question.ID, len(trace.Final), question.CorrectStoredName, hitRank >= 0, hitRank,
			rankRelevant, rankReranked, question.Query)
	}

	total := len(questions)
	result := Result{
		Cases: total, Documents: len(dataset.Documents),
		DocumentRecall: float64(hitCount) / float64(total),
		EmptyRate:      float64(emptyCount) / float64(total),
		MRR:            reciprocalRankSum / float64(total),
		RerankEligible: rerankEligible, DistanceCount: distanceCount,
	}
	if rerankEligible > 0 {
		result.RerankGain = float64(rerankGain) / float64(rerankEligible)
	}
	if distanceCount > 0 {
		result.AverageDistance = distanceSum / float64(distanceCount)
	}
	result.Passed = result.DocumentRecall >= opts.MinRecall && result.EmptyRate <= opts.MaxEmptyRate

	fmt.Fprintln(output, "--- watsonxDocsQA retrieval summary ---")
	fmt.Fprintf(output, "split=%s cases=%d documents=%d account=%s\n", opts.Split, result.Cases, result.Documents, opts.AccountNo)
	fmt.Fprintf(output, "document_recall=%.3f (threshold %.2f) empty_rate=%.3f (threshold %.2f) document_mrr=%.3f\n",
		result.DocumentRecall, opts.MinRecall, result.EmptyRate, opts.MaxEmptyRate, result.MRR)
	fmt.Fprintf(output, "rerank_gain=%.3f (n=%d) avg_distance=%.4f (n=%d)\n",
		result.RerankGain, result.RerankEligible, result.AverageDistance, result.DistanceCount)
	if result.Passed {
		fmt.Fprintln(output, "RESULT: PASS")
	} else {
		fmt.Fprintln(output, "RESULT: FAIL (document recall or empty rate missed threshold)")
	}
	return result, nil
}

func uniqueDocumentRank(candidates []domaineval.Candidate, storedName string) int {
	seen := make(map[string]struct{}, len(candidates))
	rank := 0
	for _, candidate := range candidates {
		if candidate.StoredName == "" {
			continue
		}
		if _, exists := seen[candidate.StoredName]; exists {
			continue
		}
		seen[candidate.StoredName] = struct{}{}
		if candidate.StoredName == storedName {
			return rank
		}
		rank++
	}
	return -1
}
