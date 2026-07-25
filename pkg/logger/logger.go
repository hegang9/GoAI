// Package logger 是对 log/slog 的轻量封装，为项目提供统一的结构化日志接口。
//
// 它属于跨层通用能力（cross-cutting facade），允许 application / infrastructure /
// interfaces 等各层直接调用，不引入业务耦合。
//
// 使用方式：
//
//	logger.Info("redis init success")
//	logger.Error("CreateSession failed", "err", err, "userName", userName)
//	logger.Debug("SSE chunk", "content", msg, "len", len(msg))
//	logger.Fatal("RabbitMQ connection failed", "err", err) // 会 os.Exit(1)
//
// 日志级别从低到高：Debug < Info < Warn < Error。
// 级别由 defaultLogLevel 常量控制（当前为 debug），修改后需重新编译。
//
// 输出目标：
//   - 控制台（stdout）：debug 模式为带 ANSI 颜色的易读文本；release 为 JSON
//   - logs/ 目录按日期命名的日志文件：与控制台同内容但无颜色（文件为纯文本/JSON）
//
// 单文件超过 maxLogFileSizeMB 后自动轮转，保留 30 天。
// source 字段指向业务调用方（通过 runtime.Callers 跳过本包包装层）。
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

const defaultLogDir = "logs"
const maxLogFileSizeMB = 50
const defaultLogLevel = "debug"

func ensureLogDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func newFileWriter(dir string, maxSizeMB int) (io.Writer, error) {
	if err := ensureLogDir(dir); err != nil {
		return nil, err
	}
	return rotatelogs.New(
		filepath.Join(dir, "%Y-%m-%d.log"),
		rotatelogs.WithRotationSize(int64(maxSizeMB*1024*1024)),
		rotatelogs.WithRotationTime(24*time.Hour),
		rotatelogs.WithMaxAge(30*24*time.Hour),
	)
}

// InitLogger 根据运行模式初始化全局 logger。
// 应在启动入口尽早调用（在首次打日志之前）。
//
// 输出：stdout 与 logs/%Y-%m-%d.log 双写（文件打开失败时仅写 stdout）。
// 格式由 gin.Mode() 决定：
//   - debug：彩色 Text（控制台）+ 无色 Text（文件）
//   - release / test：JSON（控制台与文件）
//
// 日志级别由 defaultLogLevel 常量决定，可选 debug、info、warn、error。
func InitLogger() {
	var level slog.Level
	switch strings.ToLower(defaultLogLevel) {
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

	fileWriter, err := newFileWriter(defaultLogDir, maxLogFileSizeMB)
	if err != nil {
		// SetDefault 之前不能用 slog，直接写 stderr 提示文件日志不可用。
		fmt.Fprintf(os.Stderr, "file logger disabled: %v\n", err)
	}

	opts := &slog.HandlerOptions{
		AddSource:   true,
		Level:       level,
		ReplaceAttr: shortSourceAttr,
	}

	handlers := make([]slog.Handler, 0, 2)
	if gin.Mode() == gin.DebugMode {
		// 控制台：彩色易读；文件：同格式无颜色，避免把 ANSI 码写入日志文件。
		handlers = append(handlers, newPrettyHandler(os.Stdout, opts, true))
		if fileWriter != nil {
			// 文件必须 color=false：编辑器不会解释 ANSI，只会显示成乱码。
			handlers = append(handlers, newPrettyHandler(fileWriter, opts, false))
		}
	} else {
		handlers = append(handlers, slog.NewJSONHandler(os.Stdout, opts))
		if fileWriter != nil {
			handlers = append(handlers, slog.NewJSONHandler(fileWriter, opts))
		}
	}

	slog.SetDefault(slog.New(multiHandler(handlers)))
}

// log 通过 runtime.Callers 跳过本包包装层，让 AddSource 指向真实业务调用方。
// skip=3：Callers 自身 + log + Info/Warn/Error/Debug。
func log(level slog.Level, msg string, args ...any) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = logger.Handler().Handle(context.Background(), r)
}

// Info 输出 Info 级别日志。args 为 key-value 对，如 "key", val, "key2", val2。
func Info(msg string, args ...any) {
	log(slog.LevelInfo, msg, args...)
}

// Error 输出 Error 级别日志。
func Error(msg string, args ...any) {
	log(slog.LevelError, msg, args...)
}

// Warn 输出 Warn 级别日志，用于可恢复的异常情况。
func Warn(msg string, args ...any) {
	log(slog.LevelWarn, msg, args...)
}

// Debug 输出 Debug 级别日志，仅在 defaultLogLevel 为 debug 时可见。
func Debug(msg string, args ...any) {
	log(slog.LevelDebug, msg, args...)
}

// Fatal 输出 Error 级别日志后调用 os.Exit(1) 终止程序。
// 仅在初始化阶段不可恢复的错误时使用。
func Fatal(msg string, args ...any) {
	log(slog.LevelError, msg, args...)
	fmt.Fprintf(os.Stderr, "FATAL: %s\n", msg)
	os.Exit(1)
}
