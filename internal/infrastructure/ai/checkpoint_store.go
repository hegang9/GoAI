package ai

import (
	"context"
	"time"

	"github.com/cloudwego/eino/adk"
	redisCli "github.com/redis/go-redis/v9"
)

const checkpointKeyPrefix = "goai:agent:checkpoint:"

// RedisCheckpointStore 实现 Eino ADK 官方 CheckPointStore。
// Runner 只在 Agent 被 interrupt 时写入状态；普通聊天请求不会产生额外 Redis 写入。
type RedisCheckpointStore struct {
	client *redisCli.Client
	ttl    time.Duration
}

// NewRedisCheckpointStore 创建带 TTL 的 Agent checkpoint 存储。
func NewRedisCheckpointStore(client *redisCli.Client, ttl time.Duration) *RedisCheckpointStore {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &RedisCheckpointStore{client: client, ttl: ttl}
}

var _ adk.CheckPointStore = (*RedisCheckpointStore)(nil)

// Get 读取 checkpoint；Redis Nil 表示不存在，不视为基础设施错误。
func (s *RedisCheckpointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	data, err := s.client.Get(ctx, checkpointKeyPrefix+checkPointID).Bytes()
	if err == redisCli.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Set 保存 Eino 序列化后的运行状态，并通过 TTL 回收长期未恢复的中断任务。
func (s *RedisCheckpointStore) Set(ctx context.Context, checkPointID string, checkpoint []byte) error {
	return s.client.Set(ctx, checkpointKeyPrefix+checkPointID, checkpoint, s.ttl).Err()
}
