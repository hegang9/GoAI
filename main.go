package main

import (
	"GopherAI/common/aihelper"
	"GopherAI/common/logger"
	"GopherAI/common/mysql"
	"GopherAI/common/rabbitmq"
	"GopherAI/common/redis"
	"GopherAI/config"
	"GopherAI/dao"
	"GopherAI/router"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// shutdownTimeout 是优雅关闭时等待在途请求处理完成的最长时间，超时后强制结束。
const shutdownTimeout = 10 * time.Second

// newHTTPServer 构建 HTTP 服务实例。
// 使用 *http.Server 而非 gin 的 r.Run，是为了能在收到退出信号时调用 Shutdown 做优雅关闭。
func newHTTPServer(addr string, port int) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf("%s:%d", addr, port),
		Handler: router.InitRouter(),
	}
}

// gracefulShutdown 按依赖反序释放资源：
// 先停止接收新请求并等待在途请求完成，再依次关闭 RabbitMQ、Redis、MySQL 连接。
func gracefulShutdown(srv *http.Server) {
	// 给在途请求预留处理时间，超时后强制结束。
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// 1. 停止 HTTP 服务：不再接收新连接，等待在途请求处理完毕。
	logger.Info("gracefulShutdown: stopping HTTP server")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("gracefulShutdown: HTTP server shutdown failed", "err", err)
	}

	// 2. 关闭 RabbitMQ：关闭连接会使消费者 goroutine 退出。
	logger.Info("gracefulShutdown: closing RabbitMQ")
	rabbitmq.DestroyRabbitMQ()

	// 3. 关闭 Redis 连接。
	logger.Info("gracefulShutdown: closing Redis")
	if err := redis.CloseRedis(); err != nil {
		logger.Error("gracefulShutdown: close Redis failed", "err", err)
	}

	// 4. 关闭 MySQL 连接池。
	logger.Info("gracefulShutdown: closing MySQL")
	if err := mysql.CloseMysql(); err != nil {
		logger.Error("gracefulShutdown: close MySQL failed", "err", err)
	}

	logger.Info("gracefulShutdown: completed")
}

// readDataFromDBAndInitHelper 从数据库加载历史消息，重建内存中的 AI 会话上下文。
//
// 一致性保证：
//   - 顺序：dao.GetAllMessages 按 created_at、id 升序返回，回放顺序与插入顺序一致；
//   - 角色：直接使用每条消息持久化的 IsUser，区分用户/AI，不做下标推断；
//   - 不重复落库：回放统一使用 save=false，仅写入内存、不再向 MQ 发布、不触发二次持久化，
//     因此重启后内存态与 DB 态保持一致，既不会重复写入也不会丢失已有数据。
func readDataFromDBAndInitHelper() error {
	// 初始化全局管理器
	manager := aihelper.GetGlobalManager()
	// 从数据库读取所有历史消息
	msgs, err := dao.GetAllMessages()
	if err != nil {
		return err
	}
	// 遍历数据库消息
	for i := range msgs {
		m := &msgs[i]
		//默认openai模型
		modelType := "1"
		// config
		c := make(map[string]interface{})

		// 为每个session恢复对应的 AIHelper
		helper, err := manager.GetOrCreateAIHelper(m.AccountNo, m.SessionID, modelType, c)
		if err != nil {
			logger.Error("readDataFromDB failed to create helper", "accountNo", m.AccountNo, "session", m.SessionID, "err", err)
			continue
		}
		logger.Debug("readDataFromDB init", "session", helper.SessionID)
		// save=false：仅重建内存上下文，不重复持久化（避免向 MQ/DB 二次写入）。
		// 保留持久化的 IsUser，确保用户/AI 角色与 DB 完全一致。
		helper.AddMessage(m.Content, m.AccountNo, m.IsUser, false)
	}

	logger.Info("AIHelperManager init success")
	return nil
}

func main() {
	logger.InitLogger()
	conf := config.GetConfig()
	host := conf.MainConfig.Host
	port := conf.MainConfig.Port
	//初始化mysql客户端
	if err := mysql.InitMysql(); err != nil {
		logger.Error("InitMysql error", "err", err)
		return
	}
	//用数据库里的历史消息回放，重建内存中的 AI 会话状态，并初始化AIHelperManager
	if err := readDataFromDBAndInitHelper(); err != nil {
		logger.Fatal("readDataFromDBAndInitHelper failed", "err", err)
		return
	}

	//初始化redis客户端
	redis.InitRedis()
	logger.Info("redis init success")
	//初始化rabbitmq客户端
	rabbitmq.InitRabbitMQ()
	logger.Info("rabbitmq init success")

	// 在独立 goroutine 中启动 HTTP 服务，主 goroutine 负责监听退出信号。
	srv := newHTTPServer(host, port)
	go func() {
		logger.Info("HTTP server starting", "addr", srv.Addr)
		// Shutdown 触发时 ListenAndServe 会返回 http.ErrServerClosed，属于正常退出，不应视为错误。
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("ListenAndServe failed", "err", err)
		}
	}()

	// 监听 SIGINT（Ctrl+C）/ SIGTERM（容器停止），收到后触发优雅关闭。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutdown signal received", "signal", sig.String())

	gracefulShutdown(srv)
}
