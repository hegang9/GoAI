// Command server 是 GopherAI 后端的主入口。
//
// 它只负责装配与生命周期编排：构建应用（bootstrap.New）、启动 HTTP 服务、
// 监听退出信号并执行优雅关闭。所有依赖装配细节位于 internal/bootstrap。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"GopherAI/internal/bootstrap"
	"GopherAI/pkg/logger"
)

// shutdownTimeout 优雅关闭时等待在途请求完成的最长时间。
const shutdownTimeout = 10 * time.Second

func main() {
	app, err := bootstrap.New()
	if err != nil {
		logger.Fatal("bootstrap failed", "err", err)
		return
	}

	app.Start()

	// 监听 SIGINT（Ctrl+C）/ SIGTERM（容器停止），收到后触发优雅关闭。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutdown signal received", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	app.Shutdown(ctx)
}
