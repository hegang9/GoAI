package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ANSI 颜色：仅用于控制台；写入文件的 Handler 不加颜色。
const (
	ansiReset  = "\033[0m"
	ansiGray   = "\033[90m"
	ansiCyan   = "\033[36m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiBold   = "\033[1m"
)

// multiHandler 把同一条 Record 扇出到多个 Handler（如控制台 + 文件）。
type multiHandler []slog.Handler

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, handler := range h {
		if !handler.Enabled(ctx, r.Level) {
			continue
		}
		// Clone：多个 Handler 可能各自消费 attrs，避免互相干扰。
		if err := handler.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(multiHandler, len(h))
	for i, handler := range h {
		next[i] = handler.WithAttrs(attrs)
	}
	return next
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	next := make(multiHandler, len(h))
	for i, handler := range h {
		next[i] = handler.WithGroup(name)
	}
	return next
}

// shortSourceAttr 把绝对路径压缩为「父目录/文件名:行号」，便于阅读。
func shortSourceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key != slog.SourceKey {
		return a
	}
	src, ok := a.Value.Any().(*slog.Source)
	if !ok || src == nil {
		return a
	}
	dir := filepath.Base(filepath.Dir(src.File))
	file := filepath.Base(src.File)
	a.Value = slog.StringValue(fmt.Sprintf("%s/%s:%d", dir, file, src.Line))
	return a
}

// levelLabel 把 slog 级别映射为固定宽度标签，便于扫读。
func levelLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN "
	case level >= slog.LevelInfo:
		return "INFO "
	default:
		return "DEBUG"
	}
}

func levelANSI(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return ansiRed + ansiBold
	case level >= slog.LevelWarn:
		return ansiYellow + ansiBold
	case level >= slog.LevelInfo:
		return ansiGreen + ansiBold
	default:
		return ansiCyan
	}
}

// prettyHandler 控制台易读输出：时间 + 彩色级别 + 短 source + msg + attrs。
type prettyHandler struct {
	w      io.Writer
	opts   slog.HandlerOptions
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
	color  bool
}

func newPrettyHandler(w io.Writer, opts *slog.HandlerOptions, color bool) *prettyHandler {
	h := &prettyHandler{w: w, color: color, mu: &sync.Mutex{}}
	if opts != nil {
		h.opts = *opts
	}
	if h.opts.Level == nil {
		h.opts.Level = slog.LevelInfo
	}
	return h
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	buf := make([]byte, 0, 256)

	ts := r.Time.Format("15:04:05.000")
	if h.color {
		buf = append(buf, ansiGray...)
		buf = append(buf, ts...)
		buf = append(buf, ansiReset...)
	} else {
		buf = append(buf, ts...)
	}
	buf = append(buf, ' ')

	label := levelLabel(r.Level)
	if h.color {
		buf = append(buf, levelANSI(r.Level)...)
		buf = append(buf, label...)
		buf = append(buf, ansiReset...)
	} else {
		buf = append(buf, label...)
	}
	buf = append(buf, ' ')

	if h.opts.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		src := fmt.Sprintf("%s/%s:%d", filepath.Base(filepath.Dir(f.File)), filepath.Base(f.File), f.Line)
		if h.color {
			buf = append(buf, ansiGray...)
			buf = append(buf, src...)
			buf = append(buf, ansiReset...)
		} else {
			buf = append(buf, src...)
		}
		buf = append(buf, ' ')
	}

	buf = append(buf, r.Message...)

	// 先写 WithAttrs 积累的属性，再写本条 Record 的属性。
	writeAttr := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		if h.opts.ReplaceAttr != nil {
			a = h.opts.ReplaceAttr(h.groups, a)
			if a.Equal(slog.Attr{}) {
				return
			}
		}
		buf = append(buf, ' ')
		if h.color {
			buf = append(buf, ansiGray...)
			buf = append(buf, a.Key...)
			buf = append(buf, ansiReset...)
		} else {
			buf = append(buf, a.Key...)
		}
		buf = append(buf, '=')
		buf = append(buf, formatAttrValue(a.Value)...)
	}
	for _, a := range h.attrs {
		writeAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a)
		return true
	})
	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := *h
	cloned.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &cloned
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	cloned := *h
	cloned.groups = append(append([]string{}, h.groups...), name)
	return &cloned
}

func formatAttrValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if strings.IndexByte(s, ' ') >= 0 || strings.IndexByte(s, '"') >= 0 {
			return fmt.Sprintf("%q", s)
		}
		return s
	case slog.KindTime:
		return v.Time().Format(time.RFC3339Nano)
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindAny:
		return fmt.Sprintf("%v", v.Any())
	default:
		return v.String()
	}
}
