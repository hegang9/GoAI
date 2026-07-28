package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"GopherAI/config"
	appbench "GopherAI/internal/application/evaluation/planbench"
	"GopherAI/internal/infrastructure/ai"
	"GopherAI/internal/infrastructure/evaluation/planneradapter"
)

// PlannerBenchmarkRuntime 持有 Planner 评测入口与可释放资源。
type PlannerBenchmarkRuntime struct {
	Engine  appbench.Planner
	adapter *planneradapter.Adapter
}

// NewPlannerBenchmarkRuntime 按 Planner 生产配置装配独立评测运行时。
func NewPlannerBenchmarkRuntime(ctx context.Context, accountNo string) (*PlannerBenchmarkRuntime, error) {
	conf := config.GetConfig()
	if conf == nil {
		return nil, errors.New("load config failed")
	}
	if !conf.PlannerConfig.Enabled {
		return nil, errors.New("plannerConfig.enabled=false, cannot benchmark planner")
	}
	planner, err := ai.NewPlanner(ctx,
		conf.PlannerConfig.ModelName,
		conf.PlannerConfig.BaseURL,
		conf.PlannerConfig.PlannerAPIKey,
		conf.PlannerConfig.HistoryWindow,
		conf.PlannerConfig.TimeoutMs)
	if err != nil {
		return nil, fmt.Errorf("create benchmark planner: %w", err)
	}
	adapter, err := planneradapter.New(planner, accountNo)
	if err != nil {
		return nil, fmt.Errorf("create planner benchmark adapter: %w", err)
	}
	return &PlannerBenchmarkRuntime{Engine: adapter, adapter: adapter}, nil
}

// Close 清理评测运行时创建的测试账号占位文档。
func (r *PlannerBenchmarkRuntime) Close() error {
	if r == nil || r.adapter == nil {
		return nil
	}
	return r.adapter.Close()
}
