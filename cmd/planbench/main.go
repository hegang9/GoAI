// Package main 是 planner 离线评测工具（cmd/planbench）。
//
// 用途：对 [plannerConfig] 驱动的 Planner 在标注评估集上批量跑决策，输出
//   - 误判率（need_retrieval 与期望不符）
//   - query 改写 ROUGE-L 均值（retrieval_query vs 期望 query_keywords 拼接）
//   - filter 提取准确率（doc_filter.storedName/headers 是否匹配）
//
// 误判率 > 15% 时以非 0 退出，便于 CI 卡门禁。
//
// 评测环境：mock 文件系统——在临时工作目录下建 uploads/{accountNo}/placeholder，
// 使 storage.HasUserDocs(accountNo) 返回 true，从而让所有评估条目都进入 planner LLM 路径，
// 专注测 planner 决策质量，不依赖 Redis、不修改 planner 结构。
//
// 用法：
//
//	go run ./cmd/planbench -evalset ./cmd/planbench/evalset.jsonl
//	go run ./cmd/planbench -evalset ./evalset.jsonl -accountNo bench_user
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"GopherAI/config"
	"GopherAI/internal/domain/chat"
	"GopherAI/internal/infrastructure/ai"
	"GopherAI/pkg/logger"
)

// evalCase 是评估集单条用例。
type evalCase struct {
	// History 多轮历史，role: "user"|"assistant"。
	History []historyMsg `json:"history"`
	// LastMessage 最后一条用户消息（评测时追加为 user 消息）。
	LastMessage string `json:"last_message"`
	// Expect 期望决策结果。
	Expect expectSpec `json:"expect"`
}

type historyMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type expectSpec struct {
	NeedRetrieval  bool       `json:"need_retrieval"`
	DocFilter      docFilter  `json:"doc_filter"`
	QueryKeywords  []string   `json:"query_keywords"`
}

type docFilter struct {
	StoredName string `json:"storedName"`
	Headers    string `json:"headers"`
}

func main() {
	var (
		evalsetPath = flag.String("evalset", "cmd/planbench/evalset.jsonl", "评估集 JSONL 路径")
		accountNo   = flag.String("accountNo", "bench_user", "评测用账号编号（用于 HasUserDocs 门禁与 planner）")
	)
	flag.Parse()

	cases, err := loadEvalset(*evalsetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load evalset failed: %v\n", err)
		os.Exit(2)
	}
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "evalset is empty")
		os.Exit(2)
	}

	// mock 文件系统：临时工作目录 + 占位文档，让 storage.HasUserDocs 返回 true。
	cleanup, err := mockDocDir(*accountNo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mock doc dir failed: %v\n", err)
		os.Exit(2)
	}
	defer cleanup()

	// 读配置并构造 planner；plannerConfig.enabled=false 时直接报错退出，
	// 因为评测目的就是测 planner 决策，没启用则无意义。
	conf := config.GetConfig()
	if conf == nil {
		fmt.Fprintln(os.Stderr, "load config failed")
		os.Exit(2)
	}
	if !conf.PlannerConfig.Enabled {
		fmt.Fprintln(os.Stderr, "plannerConfig.enabled=false, cannot benchmark planner")
		os.Exit(2)
	}
	planner, err := ai.NewPlanner(context.Background(),
		conf.PlannerConfig.ModelName,
		conf.PlannerConfig.BaseURL,
		conf.PlannerConfig.PlannerAPIKey,
		conf.PlannerConfig.HistoryWindow,
		conf.PlannerConfig.TimeoutMs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create planner failed: %v\n", err)
		os.Exit(2)
	}
	logger.Info("planbench planner ready", "model", conf.PlannerConfig.ModelName, "cases", len(cases))

	var (
		misjudged  int
		filterHit  int
		rougeSum   float64
		rougeCount int
	)
	for i, c := range cases {
		msgs := toDomainMessages(c)
		plan := planner.Plan(context.Background(), *accountNo, msgs, chat.RAGFilter{})

		// 误判率：need_retrieval 与期望不符即记一次
		if plan.NeedRetrieval != c.Expect.NeedRetrieval {
			misjudged++
		}

		// filter 准确率：storedName 与 headers 都匹配才算命中
		if plan.DocFilter.StoredName == c.Expect.DocFilter.StoredName &&
			plan.DocFilter.Headers == c.Expect.DocFilter.Headers {
			filterHit++
		}

		// query 改写 ROUGE-L：仅当期望 need_retrieval=true 且有 query_keywords 时计算
		if c.Expect.NeedRetrieval && len(c.Expect.QueryKeywords) > 0 {
			ref := joinKeywords(c.Expect.QueryKeywords)
			rougeSum += rougeL(plan.RetrievalQuery, ref)
			rougeCount++
		}

		fmt.Printf("[%d] need=%v(exp %v) src=%s conf=%s q=%q filter={%s/%s} rouge=%.3f\n",
			i, plan.NeedRetrieval, c.Expect.NeedRetrieval,
			plan.Source, plan.Confidence, plan.RetrievalQuery,
			plan.DocFilter.StoredName, plan.DocFilter.Headers,
			rougeL(plan.RetrievalQuery, joinKeywords(c.Expect.QueryKeywords)))
	}

	total := len(cases)
	misjudgeRate := float64(misjudged) / float64(total)
	filterAcc := float64(filterHit) / float64(total)
	var rougeAvg float64
	if rougeCount > 0 {
		rougeAvg = rougeSum / float64(rougeCount)
	}

	fmt.Println("--- summary ---")
	fmt.Printf("cases=%d  misjudge_rate=%.3f (threshold 0.15)\n", total, misjudgeRate)
	fmt.Printf("filter_accuracy=%.3f  rougeL_avg=%.3f (n=%d)\n", filterAcc, rougeAvg, rougeCount)

	if misjudgeRate > 0.15 {
		fmt.Println("RESULT: FAIL (misjudge rate exceeds 15%)")
		os.Exit(1)
	}
	fmt.Println("RESULT: PASS")
}

// loadEvalset 逐行读取 JSONL 评估集。
func loadEvalset(path string) ([]evalCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cases []evalCase
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}
		var c evalCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("parse line: %w", err)
		}
		cases = append(cases, c)
	}
	return cases, scanner.Err()
}

// toDomainMessages 把评估用例的 history + last_message 转为 chat.Message。
func toDomainMessages(c evalCase) []chat.Message {
	msgs := make([]chat.Message, 0, len(c.History)+1)
	for _, h := range c.History {
		msgs = append(msgs, chat.Message{Content: h.Content, IsUser: h.Role == "user"})
	}
	msgs = append(msgs, chat.Message{Content: c.LastMessage, IsUser: true})
	return msgs
}

// joinKeywords 把期望关键词拼接成参考串，供 ROUGE-L 计算。
func joinKeywords(kw []string) string {
	out := ""
	for _, k := range kw {
		out += k
	}
	return out
}

// mockDocDir 在临时目录建 uploads/{accountNo}/placeholder 并切换工作目录，
// 使 storage.HasUserDocs(accountNo) 返回 true。返回 cleanup 还原工作目录。
func mockDocDir(accountNo string) (cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "planbench-*")
	if err != nil {
		return nil, err
	}
	docDir := filepath.Join(tmp, "uploads", accountNo)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(docDir, "placeholder"), []byte("bench"), 0o644); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	orig, err := os.Getwd()
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	if err := os.Chdir(tmp); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	return func() {
		_ = os.Chdir(orig)
		_ = os.RemoveAll(tmp)
	}, nil
}
