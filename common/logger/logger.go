// Package logger 是对 log/slog 的轻量封装，为项目提供统一的结构化日志接口。
//
// 使用方式：
//
//	logger.Info("redis init success")
//	logger.Error("CreateSession failed", "err", err, "userName", userName)
//	logger.Debug("SSE chunk", "content", msg, "len", len(msg))
//	logger.Fatal("RabbitMQ connection failed", "err", err) // 会 os.Exit(1)
//
// 日志级别从低到高：Debug < Info < Warn < Error
// 默认级别为 Info（Debug 日志不输出），可通过环境变量 LOG_LEVEL=debug 开启。
//
// 输出格式：
//   - debug 模式（gin.Mode() == "debug"）：Text 格式，带源文件位置
//   - release 模式：JSON 格式，便于日志采集和检索
package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// InitLogger 根据运行模式初始化全局 logger。
// 应在 main.go 中尽早调用（在首次打日志之前）。
//
//   - debug 模式：TextHandler，输出到 stderr，格式易读
//   - release 模式：JSONHandler，输出到 stdout，字段结构化
//
// 日志级别通过环境变量 LOG_LEVEL 控制，可选值：debug、info、warn、error。
// 默认级别为 info（Debug 日志在生产环境不输出）。
func InitLogger() {
	var level slog.Level
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// HandlerOptions 配置 slog handler 的行为。
	// AddSource: true → 每条日志自动附带源文件名和行号（如 "logger.go:67"），
	// 便于定位代码，但会有微小的性能开销（生产环境可接受）。
	// Level: 设置最低输出级别，低于此级别的日志被静默丢弃。
	opts := &slog.HandlerOptions{
		AddSource: true, // 自动附加调用文件名和行号
		Level:     level,
	}

	var handler slog.Handler
	// 根据 Gin 运行模式选择日志输出格式：
	//   debug 模式（开发环境）→ TextHandler：键值对格式，人眼易读，输出到 stderr
	//   release 模式（生产环境）→ JSONHandler：结构化 JSON，方便日志采集工具解析
	if gin.Mode() == "debug" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// SetDefault 将配置好的 handler 注册为全局默认 logger。
	// 之后所有 slog.Info、slog.Error、slog.Debug、slog.Warn 调用
	// 都使用这个 handler，无需显式传递。
	slog.SetDefault(slog.New(handler))
}

// Info 输出 Info 级别日志。args 为 key-value 对，如 "key", val, "key2", val2。
func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

// Error 输出 Error 级别日志。
func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

// Warn 输出 Warn 级别日志，用于可恢复的异常情况。
func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

// Debug 输出 Debug 级别日志，仅在 LOG_LEVEL=debug 时可见。
func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// Fatal 输出 Error 级别日志后调用 os.Exit(1) 终止程序。
// 仅在初始化阶段不可恢复的错误时使用。
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	fmt.Fprintf(os.Stderr, "FATAL: %s\n", msg)
	os.Exit(1)
}
