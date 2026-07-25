package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSourceAttribution_SkipsWrapper(t *testing.T) {
	var buf bytes.Buffer
	h := newPrettyHandler(&buf, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}, false)
	slog.SetDefault(slog.New(h))

	Info("source attribution check")
	out := buf.String()

	if !strings.Contains(out, "INFO") {
		t.Fatalf("expected INFO level label, got: %q", out)
	}
	if strings.Contains(out, "logger.go:") && !strings.Contains(out, "logger_test.go:") {
		t.Fatalf("source still points to wrapper logger.go: %q", out)
	}
	if !strings.Contains(out, "logger_test.go:") {
		t.Fatalf("expected source in logger_test.go, got: %q", out)
	}
}

func TestPrettyHandler_LevelLabels(t *testing.T) {
	var buf bytes.Buffer
	h := newPrettyHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}, false)
	slog.SetDefault(slog.New(h))

	Debug("d")
	Info("i")
	Warn("w")
	Error("e")
	out := buf.String()

	for _, label := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !strings.Contains(out, label) {
			t.Fatalf("missing level label %s in: %q", label, out)
		}
	}
}
