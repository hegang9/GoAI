// Package main 提供基于 watsonxDocsQA 的 RAG 召回质量离线评测入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	appbench "GopherAI/internal/application/evaluation/ragbench"
	"GopherAI/internal/bootstrap"
	"GopherAI/internal/infrastructure/evaluation/watsonxdocsqa"
	"GopherAI/pkg/logger"
)

const defaultBenchmarkAccount = "95829666279"

type options struct {
	datasetDir   string
	split        string
	accountNo    string
	reindex      bool
	limit        int
	minRecall    float64
	maxEmpty     float64
	validateOnly bool
}

func main() {
	os.Exit(run())
}

func run() int {
	opts := parseFlags()
	dataset, err := watsonxdocsqa.Load(opts.datasetDir, opts.split)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load watsonxDocsQA failed: %v\n", err)
		return 2
	}
	if opts.validateOnly {
		fmt.Printf("watsonxDocsQA valid: split=%s documents=%d questions=%d path=%s\n",
			opts.split, len(dataset.Documents), len(dataset.Questions), opts.datasetDir)
		return 0
	}

	logger.InitLogger()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	runtime, err := bootstrap.NewRAGBenchmarkRuntime(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create rag benchmark runtime failed: %v\n", err)
		return 2
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			logger.Warn("close rag benchmark runtime failed", "err", err)
		}
	}()

	result, err := appbench.Run(ctx, runtime.Engine, dataset, appbench.Options{
		AccountNo: opts.accountNo, Split: opts.split, Reindex: opts.reindex, Limit: opts.limit,
		MinRecall: opts.minRecall, MaxEmptyRate: opts.maxEmpty,
	}, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ragbench failed: %v\n", err)
		return 2
	}
	if !result.Passed {
		return 1
	}
	return 0
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.datasetDir, "datasetDir", "dataset/watsonxDocsQA", "watsonxDocsQA 数据集根目录")
	flag.StringVar(&opts.split, "split", "test", "watsonxDocsQA 问题集：train 或 test")
	flag.StringVar(&opts.accountNo, "accountNo", defaultBenchmarkAccount, "评测账号；重建时会清空其向量索引")
	flag.BoolVar(&opts.reindex, "reindex", true, "运行前清空账号向量索引并重新索引完整 corpus")
	flag.IntVar(&opts.limit, "limit", 0, "只评测前 N 条问题；0 表示完整 split")
	flag.Float64Var(&opts.minRecall, "minRecall", 0.80, "金标准文档召回率门槛")
	flag.Float64Var(&opts.maxEmpty, "maxEmptyRate", 0.10, "空召回率门槛")
	flag.BoolVar(&opts.validateOnly, "validateOnly", false, "只校验数据集结构，不连接 Redis 或调用模型")
	flag.Parse()
	return opts
}
