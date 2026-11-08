package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	return line
}

func TestNewLoggerWritesLegacyWireWithEveryContextAttrsMember(t *testing.T) {
	var buf bytes.Buffer
	first := func(context.Context) []slog.Attr { return []slog.Attr{slog.String("trace_id", "t1")} }
	second := func(context.Context) []slog.Attr { return []slog.Attr{slog.String("tenant", "acme")} }
	logger, err := newLogger(log.Settings{
		Level: "debug", Format: "json", Wire: "legacy", Environment: "production", Service: "billing",
	}, []log.ContextAttrs{first, nil, second}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	logger.DebugContext(t.Context(), "hello")

	line := decodeLine(t, &buf)
	want := map[string]any{
		"level": "debug", "message": "hello", "service": "billing", "env": "production",
		"trace_id": "t1", "tenant": "acme",
	}
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s = %v, want %v (line %v)", key, line[key], value, line)
		}
	}
}

func TestNewLoggerRejectsAnInvalidLevel(t *testing.T) {
	if _, err := NewLogger(log.Settings{Level: "loud"}, nil); err == nil {
		t.Fatal("NewLogger accepted LOG_LEVEL=loud")
	}
}

func TestNewLoggerInstallsTheSlogDefault(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	logger, err := NewLogger(log.Settings{Format: "json", Level: "error"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slog.Default() != logger {
		t.Fatal("NewLogger did not install its logger as the slog default")
	}
}

func TestJoinWithoutMembersIsNil(t *testing.T) {
	if join(nil) != nil || join([]log.ContextAttrs{nil}) != nil {
		t.Fatal("join of no member must be nil, so core/log adds no context handler")
	}
}
