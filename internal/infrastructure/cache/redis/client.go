// Package redis 是 Redis 适配层：封装连接构建、验证码存储（实现 domain/user.CaptchaStore）
// 以及 RAG 向量索引的底层操作（供 infrastructure/rag 使用）。
//
// 它属于纯技术基础设施，不包含业务编排逻辑。
package redis

import (
	"context"
	"strconv"

	redisCli "github.com/redis/go-redis/v9"
)

// Config 描述建立 Redis 连接所需的参数。
type Config struct {
	Host     string
	Port     int
	Password string
	DB       int
}

// Connect 创建 Redis 客户端实例，并通过 Ping 在启动阶段校验连接可用性。
//
// 固定使用 RESP2 协议，避免 RESP3 的 maint_notifications 警告日志。
func Connect(ctx context.Context, cfg Config) (*redisCli.Client, error) {
	addr := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	client := redisCli.NewClient(&redisCli.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		Protocol: 2,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// Close 关闭 Redis 客户端连接，未初始化时安全返回。
func Close(client *redisCli.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}
