package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"GopherAI/config"
	appbench "GopherAI/internal/application/evaluation/ragbench"
	redisstore "GopherAI/internal/infrastructure/cache/redis"
	"GopherAI/internal/infrastructure/evaluation/ragadapter"
	raginfra "GopherAI/internal/infrastructure/rag"

	redisCli "github.com/redis/go-redis/v9"
)

// RAGBenchmarkRuntime 持有评测所需的引擎适配器与可释放资源。
type RAGBenchmarkRuntime struct {
	Engine                 appbench.Engine
	IndexConfigFingerprint string
	redis                  *redisCli.Client
	adapter                *ragadapter.Adapter
}

// NewRAGBenchmarkRuntime 按生产配置装配独立的 RAG 评测运行时。
func NewRAGBenchmarkRuntime(ctx context.Context) (*RAGBenchmarkRuntime, error) {
	conf := config.GetConfig()
	if conf == nil {
		return nil, errors.New("load config failed")
	}
	rdb, err := redisstore.Connect(ctx, redisstore.Config{
		Host: conf.RedisHost, Port: conf.RedisPort, Password: conf.RedisPassword, DB: conf.RedisDb,
	})
	if err != nil {
		return nil, fmt.Errorf("init benchmark redis failed: %w", err)
	}
	vectorStore := redisstore.NewVectorStore(rdb)
	ragCfg := ragEngineConfig(conf)
	engine, err := raginfra.NewEngine(ctx, ragCfg, vectorStore, ragReranker(conf))
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}
	adapter, err := ragadapter.New(engine, vectorStore)
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return &RAGBenchmarkRuntime{
		Engine:                 adapter,
		IndexConfigFingerprint: raginfra.IndexConfigFingerprint(ragCfg),
		redis:                  rdb,
		adapter:                adapter,
	}, nil
}

// Close 释放 Redis 连接并清理索引时使用的临时文件。
func (r *RAGBenchmarkRuntime) Close() error {
	if r == nil {
		return nil
	}
	adapterErr := r.adapter.Close()
	redisErr := r.redis.Close()
	return errors.Join(adapterErr, redisErr)
}
