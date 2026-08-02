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

// redisPingTimeoutDefault 是 [redisConfig].pingTimeoutMs 缺省/非法时的兜底值。
const redisPingTimeoutDefault = 3 * time.Second

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
	// pingTimeoutMs<=0 时回退默认 3s，保持原硬编码行为。
	pingTimeout := time.Duration(conf.RedisPingTimeoutMs) * time.Millisecond
	if pingTimeout <= 0 {
		pingTimeout = redisPingTimeoutDefault
	}
	redisCtx, redisCancel := context.WithTimeout(context.Background(), pingTimeout)
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
	// 精排器（reranker）：仅在配置开启时构造，复用统一的 AI API Key；
	// 关闭或未配置时传 nil，Engine 自动按纯向量排序运行。
	var reranker raginfra.Reranker
	if conf.RagRerankEnable {
		reranker = raginfra.NewHTTPReranker(conf.RagRerankBaseUrl, conf.RagAPIKey, conf.RagRerankModel)
		logger.Info("rag reranker init success",
			"model", conf.RagRerankModel,
			"baseURL", conf.RagRerankBaseUrl)
	}
	ragEngine, err := raginfra.NewEngine(context.Background(), raginfra.Config{
		EmbeddingModel: conf.RagEmbeddingModel,
		BaseURL:        conf.RagBaseUrl,
		APIKey:         conf.RagAPIKey,
		Dimension:      conf.RagDimension,
		ChunkSize:      conf.RagChunkSize,
		ChunkOverlap:   conf.RagChunkOverlap,
		TopK:           conf.RagTopK,
		MaxDistance:    conf.RagMaxDistance,
		RecallTopK:     conf.RagRecallTopK,
		RerankTopK:     conf.RagRerankTopK,
		RerankEnable:   conf.RagRerankEnable,
		RerankMinScore: conf.RagRerankMinScore,
		// 文档分块与索引升级（默认关闭，仅对新上传文档生效）：
		EnableSemanticChunking: conf.RagEnableSemanticChunking,
		SemanticPercentile:     conf.RagSemanticBreakpointPercentile,
		SemanticBufferSize:     conf.RagSemanticBufferSize,
		ContextWindow:          conf.RagContextWindow,
		EnableHeaderInjection:  conf.RagEnableHeaderInjection,
	}, vectorStore, reranker)
	if err != nil {
		return nil, fmt.Errorf("init rag engine failed: %w", err)
	}
	// —— Planner（检索决策器）——
	// 仅在 plannerConfig.enabled 时构造；关闭时传 nil，auto 模型退化为纯生成。
	var planner *ai.Planner
	if conf.PlannerConfig.Enabled {
		planner, err = ai.NewPlanner(context.Background(),
			conf.PlannerConfig.ModelName,
			conf.PlannerConfig.BaseURL,
			conf.PlannerConfig.PlannerAPIKey,
			conf.PlannerConfig.HistoryWindow,
			conf.PlannerConfig.TimeoutMs)
		if err != nil {
			return nil, fmt.Errorf("init planner failed: %w", err)
		}
		logger.Info("planner init success", "model", conf.PlannerConfig.ModelName)
	}

	// 工厂创建 auto / 4；auto 模型连接配置来自自洽的 [autoModelConfig]，独立于 RAG。
	// RAG 的 query 改写与 filter 意图已由 planner 接管，旧 EnableQueryRewrite / EnableFilterIntent 不再装配。
	modelFactory := ai.NewFactory(ai.FactoryConfig{
		AutoModelName: conf.AutoModelName,
		AutoBaseURL:   conf.AutoBaseURL,
		AutoAPIKey:    conf.AutoAPIKey,
		MCPBaseURL:    conf.McpConfig.BaseURL,
		Planner:       planner,
	}, ragEngine)

	// —— 消息队列（RabbitMQ）：发布端作为会话消息 Sink，消费端落库 ——
	retryTiers := make([]rabbitmq.RetryTier, 0, len(conf.RabbitmqRetryTiers))
	for _, tier := range conf.RabbitmqRetryTiers {
		retryTiers = append(retryTiers, rabbitmq.RetryTier{
			Queue:      tier.Queue,
			RoutingKey: tier.RoutingKey,
			DelayMs:    tier.DelayMs,
		})
	}

	rabbit, err := rabbitmq.Connect(rabbitmq.Config{
		Host:     conf.RabbitmqHost,
		Port:     conf.RabbitmqPort,
		Username: conf.RabbitmqUsername,
		Password: conf.RabbitmqPassword,
		Vhost:    conf.RabbitmqVhost,

		MainExchange:   conf.RabbitmqMainExchange,
		MainQueue:      conf.RabbitmqMainQueue,
		MainRoutingKey: conf.RabbitmqMainRoutingKey,

		RetryExchange:      conf.RabbitmqRetryExchange,
		RetryTiers:         retryTiers,
		RetryJitterPercent: conf.RabbitmqRetryJitterPercent,
		MaxRetries:         conf.RabbitmqMaxRetries,
		LocalRetryDelaysMs: conf.RabbitmqLocalRetryDelaysMs,

		DeadLetterExchange:   conf.RabbitmqDeadLetterExchange,
		DeadLetterQueue:      conf.RabbitmqDeadLetterQueue,
		DeadLetterRoutingKey: conf.RabbitmqDeadLetterRoutingKey,

		PrefetchCount:           conf.RabbitmqPrefetchCount,
		PublishConfirmTimeoutMs: conf.RabbitmqPublishConfirmTimeoutMs,
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
	recognizer := imageinfra.NewONNXRecognizer(conf.ImageModelPath, conf.ImageLabelPath, 224, 224)
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
// 默认值：sessionLimit=50，defaultModelType="auto"。
func normalizeChatReplayConfig(cfg config.ChatReplayConfig) chatReplayRuntimeConfig {
	out := chatReplayRuntimeConfig{
		SessionLimit:     cfg.SessionLimit,
		DefaultModelType: cfg.DefaultModelType,
	}
	if out.SessionLimit <= 0 {
		out.SessionLimit = 50
	}
	if out.DefaultModelType == "" {
		out.DefaultModelType = "auto"
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
