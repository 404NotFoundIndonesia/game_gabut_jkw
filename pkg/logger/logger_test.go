package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/404NFIDv2/bot-game-management/pkg/logger"
)

// newTestLogger returns a logger writing to a buffer for inspection.
func newTestLogger(buf *bytes.Buffer, level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: lvl}))
}

func TestNew_OutputIsJSON(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")
	l.Info("test message", "key", "value")

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v — raw: %s", err, buf.String())
	}
	if parsed["msg"] != "test message" {
		t.Errorf("msg field: got %v", parsed["msg"])
	}
	if parsed["key"] != "value" {
		t.Errorf("key field: got %v", parsed["key"])
	}
}

func TestNew_LevelFiltering_Info(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")
	l.Debug("should not appear")
	if buf.Len() > 0 {
		t.Errorf("debug log appeared with info level: %s", buf.String())
	}
}

func TestNew_LevelFiltering_Debug(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "debug")
	l.Debug("should appear")
	if buf.Len() == 0 {
		t.Errorf("debug log missing with debug level")
	}
}

func TestNew_LevelFiltering_Warn(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "warn")
	l.Info("should not appear")
	if buf.Len() > 0 {
		t.Errorf("info log appeared with warn level: %s", buf.String())
	}
	l.Warn("should appear")
	if buf.Len() == 0 {
		t.Errorf("warn log missing with warn level")
	}
}

func TestFromContext_Default(t *testing.T) {
	ctx := context.Background()
	l := logger.FromContext(ctx)
	if l == nil {
		t.Error("expected non-nil logger from empty context")
	}
}

func TestWithContext_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")
	ctx := logger.WithContext(context.Background(), l)
	got := logger.FromContext(ctx)
	if got != l {
		t.Error("FromContext did not return the logger stored by WithContext")
	}
}

func TestNew_DefaultLevelOnUnknown(t *testing.T) {
	// logger.New with unknown level should default to info (no panic).
	l := logger.New("invalid-level")
	if l == nil {
		t.Error("expected non-nil logger for unknown level")
	}
}
