// Package planbench 编排 Planner 离线评测用例与指标统计。
package planbench

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	domaineval "GopherAI/internal/domain/evaluation"
)

// Planner 定义 Planner 评测所需的决策能力。
type Planner interface {
	Plan(ctx context.Context, accountNo string, messages []domaineval.PlannerMessage) (domaineval.PlannerDecision, error)
}

// Options 控制一次 Planner 评测运行。
type Options struct {
	AccountNo string
	Split     string
	Limit     int
}

// Result 汇总一次 Planner 评测的观察指标，不包含质量门禁。
type Result struct {
	Cases          int
	MisjudgeRate   float64
	FilterAccuracy float64
	RougeLAvg      float64
	RougeCount     int
	Categories     map[string]CategoryResult
}

// CategoryResult 汇总单个用例分类的观察指标。
type CategoryResult struct {
	Cases          int
	MisjudgeRate   float64
	FilterAccuracy float64
	RougeLAvg      float64
	RougeCount     int
}

type categoryCount struct {
	cases, misjudged, filterHit, rougeCount int
	rougeSum                                float64
}

// Run 逐条调用 Planner 并输出检索决策、filter 与 query 改写的观察指标。
func Run(ctx context.Context, planner Planner, dataset domaineval.PlannerDataset, opts Options, output io.Writer) (Result, error) {
	if strings.TrimSpace(opts.AccountNo) == "" {
		return Result{}, fmt.Errorf("accountNo cannot be empty")
	}
	if opts.Limit < 0 {
		return Result{}, fmt.Errorf("limit must be >= 0")
	}
	cases := dataset.Cases
	if opts.Limit > 0 && opts.Limit < len(cases) {
		cases = cases[:opts.Limit]
	}
	if len(cases) == 0 {
		return Result{}, fmt.Errorf("planner dataset is empty")
	}

	var misjudged, filterHit, rougeCount int
	var rougeSum float64
	counts := make(map[string]*categoryCount)
	for i, plannerCase := range cases {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		decision, err := planner.Plan(ctx, opts.AccountNo, messagesFor(plannerCase))
		if err != nil {
			return Result{}, fmt.Errorf("plan case %s: %w", plannerCase.ID, err)
		}
		count := counts[plannerCase.Category]
		if count == nil {
			count = &categoryCount{}
			counts[plannerCase.Category] = count
		}
		count.cases++
		if decision.NeedRetrieval != plannerCase.Expect.NeedRetrieval {
			misjudged++
			count.misjudged++
		}
		if decision.DocFilter == plannerCase.Expect.DocFilter {
			filterHit++
			count.filterHit++
		}
		rouge := 0.0
		if plannerCase.Expect.NeedRetrieval && len(plannerCase.Expect.QueryKeywords) > 0 {
			rouge = rougeL(decision.RetrievalQuery, joinKeywords(plannerCase.Expect.QueryKeywords))
			rougeSum += rouge
			rougeCount++
			count.rougeSum += rouge
			count.rougeCount++
		}
		fmt.Fprintf(output, "[%d] id=%s category=%s need=%v(exp %v) src=%s conf=%s q=%q filter={%s/%s} rouge=%.3f\n",
			i, plannerCase.ID, plannerCase.Category, decision.NeedRetrieval, plannerCase.Expect.NeedRetrieval,
			decision.Source, decision.Confidence, decision.RetrievalQuery,
			decision.DocFilter.StoredName, decision.DocFilter.Headers, rouge)
	}

	result := Result{
		Cases:          len(cases),
		MisjudgeRate:   float64(misjudged) / float64(len(cases)),
		FilterAccuracy: float64(filterHit) / float64(len(cases)),
		RougeCount:     rougeCount,
		Categories:     make(map[string]CategoryResult, len(counts)),
	}
	if rougeCount > 0 {
		result.RougeLAvg = rougeSum / float64(rougeCount)
	}
	for category, count := range counts {
		categoryResult := CategoryResult{
			Cases:          count.cases,
			MisjudgeRate:   float64(count.misjudged) / float64(count.cases),
			FilterAccuracy: float64(count.filterHit) / float64(count.cases),
			RougeCount:     count.rougeCount,
		}
		if count.rougeCount > 0 {
			categoryResult.RougeLAvg = count.rougeSum / float64(count.rougeCount)
		}
		result.Categories[category] = categoryResult
	}

	fmt.Fprintln(output, "--- planner summary ---")
	fmt.Fprintf(output, "split=%s cases=%d account=%s\n", opts.Split, result.Cases, opts.AccountNo)
	fmt.Fprintf(output, "misjudge_rate=%.3f filter_accuracy=%.3f rougeL_avg=%.3f (n=%d)\n",
		result.MisjudgeRate, result.FilterAccuracy, result.RougeLAvg, result.RougeCount)
	categories := make([]string, 0, len(result.Categories))
	for category := range result.Categories {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		categoryResult := result.Categories[category]
		fmt.Fprintf(output, "category=%s cases=%d misjudge_rate=%.3f filter_accuracy=%.3f rougeL_avg=%.3f (n=%d)\n",
			category, categoryResult.Cases, categoryResult.MisjudgeRate, categoryResult.FilterAccuracy,
			categoryResult.RougeLAvg, categoryResult.RougeCount)
	}
	fmt.Fprintln(output, "RESULT: COMPLETE")
	return result, nil
}

func messagesFor(plannerCase domaineval.PlannerCase) []domaineval.PlannerMessage {
	messages := make([]domaineval.PlannerMessage, 0, len(plannerCase.History)+1)
	messages = append(messages, plannerCase.History...)
	return append(messages, domaineval.PlannerMessage{Role: "user", Content: plannerCase.LastMessage})
}

func joinKeywords(keywords []string) string {
	return strings.Join(keywords, " ")
}

func rougeL(candidate, reference string) float64 {
	candidateTokens := tokenize(candidate)
	referenceTokens := tokenize(reference)
	if len(candidateTokens) == 0 || len(referenceTokens) == 0 {
		return 0
	}
	lcs := lcsLen(candidateTokens, referenceTokens)
	if lcs == 0 {
		return 0
	}
	precision := float64(lcs) / float64(len(candidateTokens))
	recall := float64(lcs) / float64(len(referenceTokens))
	return 2 * precision * recall / (precision + recall)
}

func tokenize(value string) []string {
	var tokens []string
	var buffer strings.Builder
	flush := func() {
		if buffer.Len() > 0 {
			tokens = append(tokens, strings.ToLower(buffer.String()))
			buffer.Reset()
		}
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if unicode.Is(unicode.Han, r) {
				flush()
				tokens = append(tokens, string(r))
				continue
			}
			buffer.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func lcsLen(left, right []string) int {
	dp := make([][]int, len(left)+1)
	for i := range dp {
		dp[i] = make([]int, len(right)+1)
	}
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			if left[i-1] == right[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp[len(left)][len(right)]
}
