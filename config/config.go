// Package config 负责加载和管理应用的所有配置项。
//
// 配置来源：项目根目录下的 config/config.toml 文件（TOML 格式）。
// TOML（Tom's Obvious, Minimal Language）是一种人类友好的配置文件格式，
// 用 [section] 表示分段，key = value 表示键值对。
//
// 使用方式：业务代码通过 config.GetConfig() 获取全局唯一的配置实例，
// 然后访问其内嵌的子配置，如 config.GetConfig().MysqlConfig.MysqlHost。
//
// 设计特点：
//   - 单例模式：全局只有一个 Config 实例，首次调用 GetConfig() 时懒加载
//   - 嵌入式 struct：Config 直接嵌入各子配置 struct，访问时无需中间层级
//   - toml tag：struct 字段通过 toml:"xxx" 标签与 TOML 文件中的键名对应
package config

import (
	"GopherAI/pkg/logger"

	// BurntSushi/toml：Go 语言中最常用的 TOML 解析库。
	"github.com/BurntSushi/toml"
)

// ============================================================================
// 子配置 struct —— 每个 struct 对应 config.toml 中的一个 [section]
// ============================================================================

// MainConfig 应用主配置，如监听端口、应用名称。
type MainConfig struct {
	// HTTP 服务监听端口，默认 9090。
	Port int `toml:"port"`
	// 应用名称，用于日志、邮件等场景的标识。
	AppName string `toml:"appName"`
	// 服务绑定的主机地址，如 "0.0.0.0" 或 "127.0.0.1"。
	Host string `toml:"host"`
}

// EmailConfig 邮件服务配置，用于发送验证码等邮件。
// 使用 QQ 邮箱 SMTP 服务，authcode 是 QQ 邮箱的 SMTP 授权码（非登录密码）。
type EmailConfig struct {
	// QQ 邮箱 SMTP 授权码。
	Authcode string `toml:"authcode"`
	// 发件人邮箱地址。
	Email string `toml:"email"`
}

// RedisConfig Redis 连接配置。
// Redis 在本项目中承担多种角色：验证码缓存、RAG 向量存储（RediSearch）、通用缓存。
type RedisConfig struct {
	// Redis 端口，默认 6379。
	RedisPort int `toml:"port"`
	// 使用的数据库编号（0-15），用于数据隔离。
	RedisDb int `toml:"db"`
	// Redis 服务器地址。
	RedisHost string `toml:"host"`
	// Redis 连接密码，无密码则为空字符串。
	RedisPassword string `toml:"password"`
}

// MysqlConfig MySQL 数据库连接配置。
type MysqlConfig struct {
	// MySQL 端口，默认 3306。
	MysqlPort int `toml:"port"`
	// MySQL 服务器地址。
	MysqlHost string `toml:"host"`
	// 数据库用户名。
	MysqlUser string `toml:"user"`
	// 数据库密码。
	MysqlPassword string `toml:"password"`
	// 数据库名称。
	MysqlDatabaseName string `toml:"databaseName"`
	// 字符集，通常为 "utf8mb4"。
	MysqlCharset string `toml:"charset"`
}

// JwtConfig JWT（JSON Web Token）鉴权配置。
// JWT 用于用户登录后的身份维持，客户端在请求头中携带 token，
// 服务端通过 JWT 中间件解析并验证 token 的有效性。
type JwtConfig struct {
	// Token 过期时长（小时）。
	ExpireDuration int `toml:"expire_duration"`
	// JWT 签发者标识，如 "GopherAI"。
	Issuer string `toml:"issuer"`
	// JWT 主题。
	Subject string `toml:"subject"`
	// JWT 签名密钥（HMAC 对称密钥）。
	Key string `toml:"key"`
}

// Rabbitmq RabbitMQ 消息队列连接配置。
// RabbitMQ 在本项目中用于异步消息持久化：
// AI 回复消息先发送到队列，再由消费者异步写入 MySQL，解耦请求处理和数据库写入。
type Rabbitmq struct {
	// RabbitMQ 端口，默认 5672。
	RabbitmqPort int `toml:"port"`
	// RabbitMQ 服务器地址。
	RabbitmqHost string `toml:"host"`
	// RabbitMQ 登录用户名。
	RabbitmqUsername string `toml:"username"`
	// RabbitMQ 登录密码。
	RabbitmqPassword string `toml:"password"`
	// 虚拟主机（vhost），用于多租户隔离，默认 "/"。
	RabbitmqVhost string `toml:"vhost"`
}

// RagModelConfig RAG（检索增强生成）模型配置。
// RAG 流程：用户文档 → 文本分块 → 向量嵌入 → 存入 Redis → 用户提问时检索相关文档 → 增强 LLM 回答。
type RagModelConfig struct {
	// 嵌入模型名称，如阿里云 "text-embedding-v4"。
	RagEmbeddingModel string `toml:"embeddingModel"`
	// 对话模型名称，如 "qwen-turbo"。
	RagChatModelName string `toml:"chatModelName"`
	// 用户上传文档的存储目录。
	RagDocDir string `toml:"docDir"`
	// 嵌入/对话 API 的基础 URL。
	RagBaseUrl string `toml:"baseUrl"`
	// 向量维度，如 text-embedding-v4 为 1024。
	RagDimension int `toml:"dimension"`
	// RagChunkSize 单个文本块的最大字符（rune）数；<=0 时运行时默认 512。
	RagChunkSize int `toml:"chunkSize"`
	// RagChunkOverlap 相邻文本块的重叠字符（rune）数，用于维持跨块语义连续性；<0 时运行时默认 64。
	RagChunkOverlap int `toml:"chunkOverlap"`
	// RagTopK 检索时返回的最相关文本块数量；<=0 时运行时默认 5。
	RagTopK int `toml:"topK"`
	// RagMaxDistance 召回结果允许的最大向量距离（COSINE，越小越相关）；
	// 距离大于该阈值的块视为不相关被丢弃；<=0 时运行时默认 0.6。
	RagMaxDistance float64 `toml:"maxDistance"`
	// RagEnableQueryRewrite 是否在多轮对话中用 LLM 把追问改写为自包含检索 query。
	RagEnableQueryRewrite bool `toml:"enableQueryRewrite"`
}

// VoiceServiceConfig 语音服务配置（百度 TTS 文字转语音）。
type VoiceServiceConfig struct {
	// 百度 TTS API Key。
	VoiceServiceApiKey string `toml:"voiceServiceApiKey"`
	// 百度 TTS Secret Key。
	VoiceServiceSecretKey string `toml:"voiceServiceSecretKey"`
}

// AIModelConfig AI 模型通用配置。
type AIModelConfig struct {
	// AI 服务 API Key。
	APIKey string `toml:"apiKey"`
}

// ChatReplayConfig 会话历史回放配置，对应 config.toml 的 [chatReplayConfig] 段。
//
// 启动时仅预热最近 SessionLimit 个活跃会话到内存；
// 其余会话在 ChatSend / StreamToSession / GetChatHistory 首次访问时由 ensureSessionLoaded 懒加载。
type ChatReplayConfig struct {
	// SessionLimit 启动时预热的最近活跃会话数量上限（全局，非 per-user）；<=0 时运行时代码默认 50。
	SessionLimit int `toml:"sessionLimit"`
	// DefaultModelType 启动预热与查询历史时使用的默认模型类型（"1" OpenAI / "2" RAG 等）；为空时默认 "1"。
	DefaultModelType string `toml:"defaultModelType"`
}

// ============================================================================
// 聚合配置 & 全局单例
// ============================================================================

// Config 是所有子配置的聚合体，通过嵌入（embedding）方式组合。
//
// Go 的 struct 嵌入（embedding）不是继承，而是一种组合语法糖：
// 嵌入后，外层 struct 可以直接访问内层 struct 的字段。
// 例如 Config 嵌入了 MysqlConfig，则可以通过 config.MysqlHost 直接访问，
// 而不需要写 config.MysqlConfig.MysqlHost。
//
// 每个嵌入字段的 toml tag 对应 config.toml 中的 [section] 名称。
type Config struct {
	// 对应 [emailConfig] 段
	EmailConfig `toml:"emailConfig"`
	// 对应 [redisConfig] 段
	RedisConfig `toml:"redisConfig"`
	// 对应 [mysqlConfig] 段
	MysqlConfig `toml:"mysqlConfig"`
	// 对应 [jwtConfig] 段
	JwtConfig `toml:"jwtConfig"`
	// 对应 [mainConfig] 段
	MainConfig `toml:"mainConfig"`
	// 对应 [rabbitmqConfig] 段
	Rabbitmq `toml:"rabbitmqConfig"`
	// 对应 [ragModelConfig] 段
	RagModelConfig `toml:"ragModelConfig"`
	// 对应 [voiceServiceConfig] 段
	VoiceServiceConfig `toml:"voiceServiceConfig"`
	// 对应 [aiModelConfig] 段
	AIModelConfig `toml:"aiModelConfig"`
	// 对应 [chatReplayConfig] 段
	ChatReplayConfig `toml:"chatReplayConfig"`
}

// RedisKeyConfig 定义 Redis 中使用的 key 命名模板。
//
// 为什么单独定义而不是放在 config.toml 中？
// 这些 key 格式是代码级别的约定，不属于运维可变的配置项，
// 硬编码在代码中可以避免被误修改导致数据访问错乱。
// %s 占位符在使用时由 fmt.Sprintf 替换为具体的用户名。
type RedisKeyConfig struct {
	// 验证码 key 模板，如 "captcha:%s" → "captcha:zhangsan"。
	CaptchaPrefix string
	// RAG 向量索引名模板，如 "rag_docs:%s:idx"。
	IndexName string
	// RAG 向量 key 前缀模板，如 "rag_docs:%s:"。
	IndexNamePrefix string
}

// DefaultRedisKeyConfig 是 Redis key 格式的默认配置，程序启动时直接使用。
var DefaultRedisKeyConfig = RedisKeyConfig{
	CaptchaPrefix:   "captcha:%s",
	IndexName:       "rag_docs:%s:idx",
	IndexNamePrefix: "rag_docs:%s:",
}

// config 是全局唯一的配置实例（包级私有变量）。
// 使用指针类型，nil 表示尚未初始化。
var config *Config

// initConfig 从 config/config.toml 文件加载配置到全局 config 实例。
// 小写开头意味着该函数仅包内可见，外部无法绕过 GetConfig() 直接调用。
// 内部自带 nil 保护：若 config 尚未分配内存则先分配，避免 DecodeFile panic。
func initConfig() error {
	if config == nil {
		config = new(Config)
	}
	if _, err := toml.DecodeFile("config/config.toml", config); err != nil {
		logger.Fatal("config decode failed", "err", err)
		return err
	}
	return nil
}

// GetConfig 返回全局唯一的配置实例，首次调用时自动加载。
//
// 采用懒加载（Lazy Initialization）模式：
//   - 首次调用时从 TOML 文件加载并缓存
//   - 后续调用直接返回已加载的实例
//
// 注意：这个函数不是并发安全的（没有加锁）。
// 如果在多个 goroutine 中同时首次调用，可能多次执行 initConfig，
// 但在本项目的场景中，配置总是在 main goroutine 启动阶段完成，所以无实际影响。
func GetConfig() *Config {
	if config == nil {
		_ = initConfig()
	}
	return config
}
