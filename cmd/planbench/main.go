// Package main 提供 Planner 离线评测入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	appbench "GopherAI/internal/application/evaluation/planbench"
	"GopherAI/internal/bootstrap"
	plannerdataset "GopherAI/internal/infrastructure/evaluation/plannerbench"
	"GopherAI/pkg/logger"
)

const defaultBenchmarkAccount = "planner_bench"

type options struct {
	datasetDir   string
	evalsetPath  string
	split        string
	accountNo    string
	limit        int
	validateOnly bool
}

func main() {
	os.Exit(run())
}

func run() int {
	opts := parseFlags()
	dataset, err := plannerdataset.Load(opts.datasetDir, opts.evalsetPath, opts.split)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load planner evalset failed: %v\n", err)
		return 2
	}
	if opts.validateOnly {
		fmt.Printf("planner evalset valid: split=%s cases=%d path=%s\n", opts.split, len(dataset.Cases), opts.evalsetPath)
		return 0
	}

	logger.InitLogger()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	runtime, err := bootstrap.NewPlannerBenchmarkRuntime(ctx, opts.accountNo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create planner benchmark runtime failed: %v\n", err)
		return 2
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			logger.Warn("close planner benchmark runtime failed", "err", err)
		}
	}()

	if _, err := appbench.Run(ctx, runtime.Engine, dataset, appbench.Options{
		AccountNo: opts.accountNo,
		Split:     opts.split,
		Limit:     opts.limit,
	}, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "planbench failed: %v\n", err)
		return 2
	}
	return 0
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.datasetDir, "datasetDir", "dataset/watsonxDocsQA", "watsonxDocsQA 数据集根目录")
	flag.StringVar(&opts.evalsetPath, "evalset", "testdata/planbench/evalset.jsonl", "Planner 评测集 JSONL 路径")
	flag.StringVar(&opts.split, "split", "test", "评测集 split：train 或 test")
	flag.StringVar(&opts.accountNo, "accountNo", defaultBenchmarkAccount, "评测专用账号；目录必须不存在或为空")
	flag.IntVar(&opts.limit, "limit", 0, "只评测前 N 条；0 表示完整 split")
	flag.BoolVar(&opts.validateOnly, "validateOnly", false, "只校验评测集，不调用 Planner")
	flag.Parse()
	return opts
}
