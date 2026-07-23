// Package main 是 RAG 召回质量离线评测工具（cmd/ragbench）。
//
// 用途：对 engine.RetrieveDetail 在标注评估集上批量跑召回，输出
//   - 召回率（期望块是否出现在 Final）
//   - 空召回率（Final 为空的比例）
//   - 精排增益（期望块在 Reranked 排名优于 Relevant 的比例）
//   - 平均距离 / 精排分（趋势观察）
//
// 门禁：召回率 < 80% 或空召回率 > 10% 时以非 0 退出。
//
// 评测连真实 Redis + embedding（向量召回无法 mock），不进 CI。
// 每条用例独立 accountNo（bench_{i}），跑前 DeleteAll + Index 该条 corpus，隔离干净。
//
// 用法：
//
//	docker compose up -d   # 起 Redis Stack
//	go run ./cmd/ragbench -evalset ./cmd/ragbench/evalset.jsonl
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"GopherAI/config"
	redisstore "GopherAI/internal/infrastructure/cache/redis"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/pkg/logger"
)

// evalCase 是召回评估集单条用例。
type evalCase struct {
	// Query 检索 query（已改写好的，不经过 planner）。
	Query string `json:"query"`
	// Corpus 该用例的语料，逐文档写临时文件后 Index。
	Corpus []corpusDoc `json:"corpus"`
	// Filter 可选 RetrieveFilter。
	Filter retrieveFilter `json:"filter"`
	// Expect 期望命中结果。
	Expect expectSpec `json:"expect"`
}

type corpusDoc struct {
	StoredName string `json:"storedName"`
	Content    string `json:"content"`
}

type retrieveFilter struct {
	StoredName string `json:"storedName"`
	Headers    string `json:"headers"`
}

type expectSpec struct {
	HitStoredName string   `json:"hit_storedName"`
	HitKeywords  []string `json:"hit_keywords"`
	MinHitCount  int      `json:"min_hit_count"`
}

func main() {
	var (
		evalsetPath = flag.String("evalset", "cmd/ragbench/evalset.jsonl", "评估集 JSONL 路径")
		accountPrefix = flag.String("accountPrefix", "bench_", "评测用账号前缀，实际 accountNo = prefix+i")
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

	conf := config.GetConfig()
	if conf == nil {
		fmt.Fprintln(os.Stderr, "load config failed")
		os.Exit(2)
	}

	ctx := context.Background()

	// 连 Redis + 构造 VectorStore。
	rdb, err := redisstore.Connect(ctx, redisstore.Config{
		Host:     conf.RedisHost,
		Port:     conf.RedisPort,
		Password: conf.RedisPassword,
		DB:       conf.RedisDb,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect redis failed: %v\n", err)
		os.Exit(2)
	}
	vectorStore := redisstore.NewVectorStore(rdb)

	// 构造 Engine；按配置决定是否注入 reranker（对齐 bootstrap 装配）。
	var reranker raginfra.Reranker
	if conf.RagRerankEnable {
		reranker = raginfra.NewHTTPReranker(conf.RagRerankBaseUrl, conf.RagAPIKey, conf.RagRerankModel)
	}
	engine, err := raginfra.NewEngine(ctx, raginfra.Config{
		EmbeddingModel:         conf.RagEmbeddingModel,
		BaseURL:                conf.RagBaseUrl,
		APIKey:                 conf.RagAPIKey,
		Dimension:              conf.RagDimension,
		ChunkSize:              conf.RagChunkSize,
		ChunkOverlap:           conf.RagChunkOverlap,
		TopK:                   conf.RagTopK,
		MaxDistance:            conf.RagMaxDistance,
		RecallTopK:             conf.RagRecallTopK,
		RerankTopK:             conf.RagRerankTopK,
		RerankEnable:           conf.RagRerankEnable,
		RerankMinScore:         conf.RagRerankMinScore,
		EnableSemanticChunking: conf.RagEnableSemanticChunking,
		SemanticPercentile:     conf.RagSemanticBreakpointPercentile,
		SemanticBufferSize:     conf.RagSemanticBufferSize,
		ContextWindow:          conf.RagContextWindow,
		EnableHeaderInjection:  conf.RagEnableHeaderInjection,
	}, vectorStore, reranker)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create rag engine failed: %v\n", err)
		os.Exit(2)
	}
	logger.Info("ragbench engine ready",
		"cases", len(cases), "rerankEnable", conf.RagRerankEnable)

	var (
		hitCount      int
		emptyCount    int
		rerankGain    int
		rerankEligible int
		distSum       float64
		distCnt       int
	)

	for i, c := range cases {
		accountNo := fmt.Sprintf("%s%d", *accountPrefix, i)

		// 清理上次残留 + 索引该条 corpus。
		if err := engine.DeleteAll(ctx, accountNo); err != nil {
			fmt.Fprintf(os.Stderr, "[%d] DeleteAll failed: %v\n", i, err)
			continue
		}
		tmpDir, err := os.MkdirTemp("", "ragbench-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d] MkdirTemp failed: %v\n", i, err)
			continue
		}
		indexed := 0
		for _, doc := range c.Corpus {
			p := filepath.Join(tmpDir, doc.StoredName)
			if err := os.WriteFile(p, []byte(doc.Content), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "[%d] write corpus failed: %v\n", i, err)
				continue
			}
			if err := engine.Index(ctx, accountNo, doc.StoredName, p); err != nil {
				fmt.Fprintf(os.Stderr, "[%d] Index %s failed: %v\n", i, doc.StoredName, err)
				continue
			}
			indexed++
		}

		// 检索并取明细。
		detail, err := engine.RetrieveDetail(ctx, accountNo, c.Query, raginfra.RetrieveFilter{
			StoredName: c.Filter.StoredName,
			Headers:    c.Filter.Headers,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d] RetrieveDetail failed: %v\n", i, err)
			os.RemoveAll(tmpDir)
			continue
		}
		os.RemoveAll(tmpDir)

		// 判定命中：Final 中存在块 storedName 匹配且 content 含所有 hit_keywords。
		hit := false
		hitRank := -1
		for j, d := range detail.Final {
			if d.StoredName != c.Expect.HitStoredName {
				continue
			}
			if !containsAll(d.Content, c.Expect.HitKeywords) {
				continue
			}
			hit = true
			hitRank = j
			break
		}
		if hit {
			hitCount++
		}
		if len(detail.Final) == 0 {
			emptyCount++
		}

		// 精排增益：期望块在 Reranked 的排名优于在 Relevant 的排名。
		rankRelevant := findRank(detail.Relevant, c.Expect.HitStoredName, c.Expect.HitKeywords)
		rankReranked := findRank(detail.Reranked, c.Expect.HitStoredName, c.Expect.HitKeywords)
		if rankRelevant >= 0 && rankReranked >= 0 {
			rerankEligible++
			if rankReranked < rankRelevant {
				rerankGain++
			}
		}

		// 距离统计（Final 块）。
		for _, d := range detail.Final {
			if d.Distance >= 0 {
				distSum += d.Distance
				distCnt++
			}
		}

		fmt.Printf("[%d] query=%q indexed=%d retrieved=%d final=%d hit=%v(hitRank=%d) rerank(rel=%d,rr=%d)\n",
			i, c.Query, indexed, len(detail.Retrieved), len(detail.Final), hit, hitRank, rankRelevant, rankReranked)
	}

	total := len(cases)
	recallRate := float64(hitCount) / float64(total)
	emptyRate := float64(emptyCount) / float64(total)
	var rerankGainRate, avgDist float64
	if rerankEligible > 0 {
		rerankGainRate = float64(rerankGain) / float64(rerankEligible)
	}
	if distCnt > 0 {
		avgDist = distSum / float64(distCnt)
	}

	fmt.Println("--- summary ---")
	fmt.Printf("cases=%d  recall_rate=%.3f (threshold 0.80)  empty_rate=%.3f (threshold 0.10)\n",
		total, recallRate, emptyRate)
	fmt.Printf("rerank_gain=%.3f (n=%d)  avg_distance=%.4f (n=%d)\n",
		rerankGainRate, rerankEligible, avgDist, distCnt)

	if recallRate < 0.80 || emptyRate > 0.10 {
		fmt.Println("RESULT: FAIL (recall < 80% or empty > 10%)")
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
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
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

// containsAll 判断 s 是否包含所有 keywords（子串匹配，大小写敏感）。
func containsAll(s string, keywords []string) bool {
	for _, k := range keywords {
		if k == "" {
			continue
		}
		if !strings.Contains(s, k) {
			return false
		}
	}
	return true
}

// findRank 返回满足 storedName 匹配且 content 含所有 keywords 的块在列表中的排名（0 起），未命中返回 -1。
func findRank(docs []raginfra.DocScore, storedName string, keywords []string) int {
	for i, d := range docs {
		if storedName != "" && d.StoredName != storedName {
			continue
		}
		if !containsAll(d.Content, keywords) {
			continue
		}
		return i
	}
	return -1
}
