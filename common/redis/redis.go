// Package redis 封装 Redis 客户端的初始化及常用操作。
//
// 本项目中 Redis 承担三种角色：
//  1. 邮箱验证码缓存（带 TTL 自动过期）
//  2. RAG 向量存储（通过 RediSearch 模块的 FLAT 索引 + COSINE 距离）
//  3. 通用键值缓存
//
// 全局变量 Rdb 在 Init() 中初始化，之后所有操作复用同一个连接实例。
// 连接参数从 config.toml 的 [redisConfig] 段读取。
//
// 注意：RAG 索引操作依赖 Redis 的 RediSearch 模块（FT.CREATE / FT.INFO / FT.DROPINDEX），
// 标准 Redis 镜像不含此模块，需要使用 redis-stack 或 redis-stack-server 镜像。
package redis

import (
	"GopherAI/config"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	redisCli "github.com/redis/go-redis/v9" // 导入时用别名 redisCli 避免与当前包名 redis 冲突
)

// Rdb 是全局唯一的 Redis 客户端实例（包级私有，外部可通过包名访问：redis.Rdb）。
// 在 Init() 中创建连接，之后所有操作都通过 Rdb 进行。
var Rdb *redisCli.Client

// ctx 是一个永不取消的背景上下文，用于所有 Redis 操作。
// 在实际使用中，更好的做法是让函数接受 ctx 参数而非使用包级变量，
// 以便调用方控制超时和取消，但当前简单场景下包级 ctx 足够。
var ctx = context.Background()

// Init 初始化 Redis 客户端连接。
//
// 从 config.toml 读取 host、port、password、db 四个参数，
// 创建 go-redis 客户端并赋值给全局变量 Rdb。
//
// 注意：此函数不验证连接是否成功（未调用 Ping），
// 实际的连接检测延迟到首次 Redis 操作时。
func Init() {
	conf := config.GetConfig()
	host := conf.RedisConfig.RedisHost         // Redis 服务器地址
	port := conf.RedisConfig.RedisPort         // Redis 端口，默认 6379
	password := conf.RedisConfig.RedisPassword // 连接密码，无密码则为空字符串
	db := conf.RedisDb                         // 数据库编号（0-15），用于数据隔离
	addr := host + ":" + strconv.Itoa(port)    // 拼接为 "host:port" 格式

	Rdb = redisCli.NewClient(&redisCli.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		Protocol: 2, // 固定使用 RESP2 协议，避免 RESP3 的 maint_notifications 警告日志
	})
}

// ============================================================================
// 验证码相关操作
// ============================================================================

// SetCaptchaForEmail 将邮箱验证码存入 Redis，2 分钟后自动过期。
//
// key 由 GenerateCaptcha(email) 生成，格式为 "captcha:邮箱地址"。
// 过期时间 2 分钟是验证码的典型有效期限，兼顾安全性和用户体验。
func SetCaptchaForEmail(email, captcha string) error {
	key := GenerateCaptcha(email)
	expire := 2 * time.Minute
	return Rdb.Set(ctx, key, captcha, expire).Err()
}

// CheckCaptchaForEmail 校验用户输入的验证码是否正确。
//
// 流程：
//  1. 从 Redis 获取存储的验证码
//  2. 若 key 不存在（已过期或从未发送），返回 false
//  3. 若存在，使用不区分大小写的比较（strings.EqualFold）
//  4. 校验成功后立即删除 key（一次性验证码，防止重复使用）
//  5. 校验失败返回 false，验证码保留直到过期
//
// 返回值：(是否匹配, 错误)。key 不存在时返回 (false, nil)，即视作验证码错误。
func CheckCaptchaForEmail(email, userInput string) (bool, error) {
	key := GenerateCaptcha(email)

	storedCaptcha, err := Rdb.Get(ctx, key).Result()
	if err != nil {
		// redis.Nil 表示 key 不存在（验证码已过期或从未发送）
		if err == redisCli.Nil {
			return false, nil
		}
		// 其他 Redis 错误（网络问题等），返回错误让调用方处理
		return false, err
	}

	// EqualFold 不区分大小写，避免用户输入时的烦扰
	if strings.EqualFold(storedCaptcha, userInput) {
		// 验证成功立即删除 key，确保验证码只能使用一次
		if err := Rdb.Del(ctx, key).Err(); err != nil {
			// 删除失败不影响验证结果（key 会在 2 分钟后自动过期）
		}
		return true, nil
	}

	return false, nil
}

// ============================================================================
// RediSearch 向量索引操作（RAG 功能依赖）
// ============================================================================

// InitRedisIndex 为指定文件创建 RediSearch 向量索引。
//
// 索引配置：
//   - 索引类型：FLAT（暴力搜索，数据量小时精度最高）
//   - 距离度量：COSINE（余弦相似度）
//   - 向量类型：FLOAT32
//   - 索引维度：由调用方传入的 dimension 参数决定（如 text-embedding-v4 的 1024）
//
// 如果索引已存在则跳过创建（幂等操作）。
// 通过 key 前缀实现按文件隔离索引，不同文件的向量互不干扰。
func InitRedisIndex(ctx context.Context, filename string, dimension int) error {
	indexName := GenerateIndexName(filename)

	// 先检查索引是否已存在（FT.INFO 命令查询索引元信息）
	_, err := Rdb.Do(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		fmt.Println("索引已存在，跳过创建")
		return nil
	}

	// 如果错误不是 "Unknown index name"，说明是真错误而非索引不存在
	if !strings.Contains(err.Error(), "Unknown index name") {
		return fmt.Errorf("检查索引失败: %w", err)
	}

	fmt.Println("正在创建 Redis 索引...")

	prefix := GenerateIndexNamePrefix(filename)

	// FT.CREATE 参数格式（RediSearch 2.x 语法）：
	//   FT.CREATE <index> ON HASH PREFIX <n> <prefix> SCHEMA <field> <type> ...
	//
	// 字段说明：
	//   content  → TEXT 类型，存储文档原始文本片段，用于关键词搜索
	//   metadata → TEXT 类型，存储元信息（文件名等）
	//   vector   → VECTOR 类型，FLAT 算法，FLOAT32，指定维度和余弦距离
	createArgs := []interface{}{
		"FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", prefix, // 只索引此前缀的 key
		"SCHEMA",
		"content", "TEXT",
		"metadata", "TEXT",
		"vector", "VECTOR", "FLAT",
		"6", // VECTOR 子参数数量（FLAT 固定 6 个）
		"TYPE", "FLOAT32",
		"DIM", dimension,
		"DISTANCE_METRIC", "COSINE",
	}

	if err := Rdb.Do(ctx, createArgs...).Err(); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}

	fmt.Println("索引创建成功！")
	return nil
}

// DeleteRedisIndex 删除指定文件的 RediSearch 向量索引。
//
// 在用户删除上传文件时调用，清理对应的向量数据，
// 避免残留索引占用内存和影响后续查询。
func DeleteRedisIndex(ctx context.Context, filename string) error {
	indexName := GenerateIndexName(filename)

	// FT.DROPINDEX 会同时删除索引结构和关联的所有向量数据
	if err := Rdb.Do(ctx, "FT.DROPINDEX", indexName).Err(); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}

	fmt.Println("索引删除成功！")
	return nil
}
