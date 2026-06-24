// Package bootstrap 负责依赖装配与应用生命周期管理。
//
// 它是组合根（Composition Root）：在此处自上而下创建并连接各层组件
// （基础设施适配器 -> 应用服务 -> 接口处理器 -> HTTP 路由），
// 取代旧的全局单例（如 aihelper 管理器、mysql.DB、redis.Rdb、rabbitmq.RMQMessage）。
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"GopherAI/config"
	"GopherAI/internal/application/chat"
	fileapp "GopherAI/internal/application/file"
	imageapp "GopherAI/internal/application/image"
	ttsapp "GopherAI/internal/application/tts"
	userapp "GopherAI/internal/application/user"
	domainchat "GopherAI/internal/domain/chat"
	"GopherAI/internal/infrastructure/ai"
	redisstore "GopherAI/internal/infrastructure/cache/redis"
	"GopherAI/internal/infrastructure/email"
	imageinfra "GopherAI/internal/infrastructure/image"
	"GopherAI/internal/infrastructure/mq/rabbitmq"
	"GopherAI/internal/infrastructure/persistence"
	raginfra "GopherAI/internal/infrastructure/rag"
	"GopherAI/internal/infrastructure/security"
	"GopherAI/internal/infrastructure/storage"
	ttsinfra "GopherAI/internal/infrastructure/tts"
	"GopherAI/internal/interfaces/http/controller"
	"GopherAI/internal/interfaces/http/router"
	"GopherAI/pkg/logger"

	"github.com/gin-gonic/gin"
	redisCli "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 消息队列名与图像识别模型资源路径。集中在组合根声明，便于后续收敛到配置。
const (
	messageQueueName = "Message"
	mcpBaseURL       = "http://localhost:8081/mcp"
	imageModelPath   = "/root/models/mobilenetv2/mobilenetv2-7.onnx"
	imageLabelPath   = "/root/imagenet_classes.txt"
	redisPingTimeout = 3 * time.Second
)

// App 持有运行期组件与可释放资源句柄，支撑启动与优雅关闭。
type App struct {
	server *http.Server
	rabbit *rabbitmq.Client
	redis  *redisCli.Client
	db     *gorm.DB
}

// New 装配全部依赖并返回可运行的应用实例。
func New() (*App, error) {
	logger.InitLogger()
	conf := config.GetConfig()
	if conf == nil {
		return nil, errors.New("load config failed")
	}

	// —— 持久化（MySQL） ——
	db, err := persistence.Connect(persistence.Config{
		Host:     conf.MysqlHost,
		Port:     conf.MysqlPort,
		User:     conf.MysqlUser,
		Password: conf.MysqlPassword,
		DBName:   conf.MysqlDatabaseName,
		Charset:  conf.MysqlCharset,
		Debug:    gin.Mode() == gin.DebugMode,
	})
	if err != nil {
		return nil, fmt.Errorf("init mysql failed: %w", err)
	}
	logger.Info("init mysql success")
	// 创建仓储对象，目前全部使用MYSQL
	userRepo := persistence.NewUserRepository(db)
	sessionRepo := persistence.NewSessionRepository(db)
	messageRepo := persistence.NewMessageRepository(db)

	// —— 缓存（Redis）：验证码存储 + 向量索引存储 ——
	// redisCtx 用于限制启动阶段 Redis Ping 的最长等待时间，避免依赖异常时启动过程长时间阻塞。
	redisCtx, redisCancel := context.WithTimeout(context.Background(), redisPingTimeout)
	defer redisCancel()
	rdb, err := redisstore.Connect(redisCtx, redisstore.Config{
		Host:     conf.RedisHost,
		Port:     conf.RedisPort,
		Password: conf.RedisPassword,
		DB:       conf.RedisDb,
	})
	if err != nil {
		return nil, fmt.Errorf("init redis failed: %w", err)
	}
	logger.Info("redis init success")
	captchaStore := redisstore.NewCaptchaStore(rdb)
	vectorStore := redisstore.NewVectorStore(rdb)

	// —— RAG 引擎 + AI 模型工厂 ——
	ragEngine, err := raginfra.NewEngine(context.Background(), raginfra.Config{
		EmbeddingModel: conf.RagEmbeddingModel,
		BaseURL:        conf.RagBaseUrl,
		APIKey:         conf.AIModelConfig.APIKey,
		Dimension:      conf.RagDimension,
		ChunkSize:      conf.RagChunkSize,
		ChunkOverlap:   conf.RagChunkOverlap,
		TopK:           conf.RagTopK,
		MaxDistance:    conf.RagMaxDistance,
	}, vectorStore)
	if err != nil {
		return nil, fmt.Errorf("init rag engine failed: %w", err)
	}
	modelFactory := ai.NewFactory(ai.FactoryConfig{
		OpenAIModelName:    conf.AIModelName,
		OpenAIBaseURL:      conf.AIBaseURL,
		ChatModelName:      conf.RagChatModelName,
		BaseURL:            conf.RagBaseUrl,
		APIKey:             conf.AIModelConfig.APIKey,
		MCPBaseURL:         mcpBaseURL,
		EnableQueryRewrite: conf.RagEnableQueryRewrite,
	}, ragEngine)

	// —— 消息队列（RabbitMQ）：发布端作为会话消息 Sink，消费端落库 ——
	rabbit, err := rabbitmq.Connect(rabbitmq.Config{
		Host:     conf.RabbitmqHost,
		Port:     conf.RabbitmqPort,
		Username: conf.RabbitmqUsername,
		Password: conf.RabbitmqPassword,
		Vhost:    conf.RabbitmqVhost,
		Queue:    messageQueueName,
	})
	if err != nil {
		return nil, fmt.Errorf("init rabbitmq failed: %w", err)
	}
	publisher := rabbitmq.NewPublisher(rabbit)
	logger.Info("rabbitmq publisher init success")

	// —— 会话领域管理器（取代全局单例） ——
	manager := domainchat.NewManager(modelFactory, publisher)

	// 启动时仅回放最近 N 个活跃会话，其余会话在运行时按需懒加载。
	replayCfg := normalizeChatReplayConfig(conf.ChatReplayConfig)
	if err := replayRecentSessions(manager, sessionRepo, messageRepo, replayCfg); err != nil {
		return nil, fmt.Errorf("replay recent sessions failed: %w", err)
	}

	// 启动消费者：把队列中的消息落库（消费端 -> 仓储）。
	consumer := rabbitmq.NewConsumer(rabbit, func(ctx context.Context, msg domainchat.Message) error {
		return messageRepo.Create(ctx, msg)
	})
	consumer.Start()
	logger.Info("rabbitmq consumer init success")

	// —— 其余基础设施适配器 ——
	hasher := security.NewBcryptHasher()
	issuer := security.NewJWTIssuer(security.JWTConfig{
		Key:            conf.Key,
		Issuer:         conf.Issuer,
		Subject:        conf.Subject,
		ExpireDuration: conf.ExpireDuration,
	})
	mailer := email.NewMailer(conf.EmailConfig.Email, conf.EmailConfig.Authcode)
	docStorage := storage.NewLocalDocStorage()
	recognizer := imageinfra.NewONNXRecognizer(imageModelPath, imageLabelPath, 224, 224)
	synthesizer := ttsinfra.NewBaiduSynthesizer(conf.VoiceServiceApiKey, conf.VoiceServiceSecretKey)

	// —— 应用服务 ——
	userSvc := userapp.NewService(userRepo, hasher, issuer, captchaStore, mailer)
	chatSvc := chat.NewService(manager, sessionRepo, messageRepo, replayCfg.DefaultModelType)
	fileSvc := fileapp.NewService(docStorage, ragEngine)
	imageSvc := imageapp.NewService(recognizer)
	ttsSvc := ttsapp.NewService(synthesizer)

	// —— 接口层（处理器 + 路由） ——
	// 聚合应用服务对象
	handlers := controller.NewHandlers(userSvc, chatSvc, fileSvc, imageSvc, ttsSvc)
	// 路由引擎
	engine := router.New(handlers, issuer)

	// http服务对象
	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", conf.Host, conf.Port),
		Handler: engine,
	}

	return &App{server: server, rabbit: rabbit, redis: rdb, db: db}, nil
}

// Start 在独立 goroutine 中启动 HTTP 服务。
func (a *App) Start() {
	go func() {
		logger.Info("HTTP server starting", "addr", a.server.Addr)
		// Shutdown 触发时返回 ErrServerClosed，属正常退出。
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("ListenAndServe failed", "err", err)
		}
	}()
}

// Shutdown 按依赖反序释放资源：先停 HTTP，再关 RabbitMQ、Redis、MySQL。
func (a *App) Shutdown(ctx context.Context) {
	logger.Info("shutdown: stopping HTTP server")
	if err := a.server.Shutdown(ctx); err != nil {
		logger.Error("shutdown: HTTP server shutdown failed", "err", err)
	}

	logger.Info("shutdown: closing RabbitMQ")
	if err := a.rabbit.Close(); err != nil {
		logger.Error("shutdown: close rabbitmq failed", "err", err)
	}

	logger.Info("shutdown: closing Redis")
	if err := redisstore.Close(a.redis); err != nil {
		logger.Error("shutdown: close Redis failed", "err", err)
	}

	logger.Info("shutdown: closing MySQL")
	if err := persistence.Close(a.db); err != nil {
		logger.Error("shutdown: close MySQL failed", "err", err)
	}

	logger.Info("shutdown: completed")
}

// chatReplayRuntimeConfig 是启动回放阶段的运行时配置，由 normalizeChatReplayConfig 填充默认值后生成。
type chatReplayRuntimeConfig struct {
	// SessionLimit 启动时预热的最近活跃会话数量上限（全局）。
	SessionLimit int
	// DefaultModelType 预热回放时创建 Conversation 使用的默认模型类型。
	DefaultModelType string
}

// normalizeChatReplayConfig 读取 TOML 配置并填充回放策略的默认值。
//
// 默认值：sessionLimit=50，defaultModelType="1"。
func normalizeChatReplayConfig(cfg config.ChatReplayConfig) chatReplayRuntimeConfig {
	out := chatReplayRuntimeConfig{
		SessionLimit:     cfg.SessionLimit,
		DefaultModelType: cfg.DefaultModelType,
	}
	if out.SessionLimit <= 0 {
		out.SessionLimit = 50
	}
	if out.DefaultModelType == "" {
		out.DefaultModelType = "1"
	}
	return out
}

// replayRecentSessions：仅将最近 N 个活跃会话预热到内存。
//
// 执行步骤：
//  1. sessionRepo.ListRecent 按消息最后活跃时间取全局 top N 会话；
//  2. 对每个会话 messageRepo.ListBySession 拉取完整消息历史；
//  3. manager.ReplayMessages 以 persist=false 写入内存，不触发 MQ。
//
// 未入选的冷会话不在启动阶段加载，留待应用层 ensureSessionLoaded 在首次访问时懒加载。
//
// 一致性保证：
//   - 顺序：ListBySession 按 created_at、id 升序返回，回放顺序与插入顺序一致；
//   - 角色：使用每条消息持久化的 IsUser，不做下标推断；
//   - 不重复落库：回放统一 persist=false，仅写内存、不再发布到 MQ，避免二次持久化。
func replayRecentSessions(
	manager *domainchat.Manager,
	sessionRepo domainchat.SessionRepository,
	messageRepo domainchat.MessageRepository,
	cfg chatReplayRuntimeConfig,
) error {
	ctx := context.Background()
	start := time.Now()

	sessions, err := sessionRepo.ListRecent(ctx, cfg.SessionLimit)
	if err != nil {
		logger.Error("replayRecentSessions ListRecent failed", "err", err)
		return err
	}

	// 统计实际回放成功的会话数与消息条数，便于启动阶段观测预热效果。
	var replayedSessions, totalMsgs int
	for _, sess := range sessions {
		msgs, err := messageRepo.ListBySession(ctx, sess.AccountNo, sess.ID)
		if err != nil {
			logger.Error("replayRecentSessions ListBySession failed",
				"accountNo", sess.AccountNo, "sessionID", sess.ID, "err", err)
			continue
		}
		if len(msgs) == 0 {
			continue
		}

		params := map[string]any{"account_no": sess.AccountNo}
		if err := manager.ReplayMessages(ctx, sess.AccountNo, sess.ID, cfg.DefaultModelType, params, msgs); err != nil {
			logger.Error("replayRecentSessions ReplayMessages failed",
				"accountNo", sess.AccountNo, "sessionID", sess.ID, "err", err)
			continue
		}
		replayedSessions++
		totalMsgs += len(msgs)
	}

	logger.Info("replayRecentSessions completed",
		"sessionLimit", cfg.SessionLimit,
		"sessionsSelected", len(sessions),
		"sessionsReplayed", replayedSessions,
		"messagesReplayed", totalMsgs,
		"duration", time.Since(start))
	return nil
}
