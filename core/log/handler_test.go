package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// syncBuffer is a bytes.Buffer safe for the concurrent writes of a handler.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// jsonLines decodes one JSON object per line.
func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.Lines(out) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		lines = append(lines, m)
	}
	return lines
}

func onlyLine(t *testing.T, out string) map[string]any {
	t.Helper()
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d:\n%s", len(lines), out)
	}
	return lines[0]
}

// The trace ids a fake tracer puts on the context. tracing.LogAttrs, which
// production passes as Config.ContextAttrs, is tested in pkg/tracing; these
// tests check only where the handler puts what ContextAttrs returns.
const (
	testTraceID = "0af7651916cd43dd8448eb211c80319c"
	testSpanID  = "b7ad6b7169203331"
)

type spanKey struct{}

func withSpan(ctx context.Context) context.Context {
	return context.WithValue(ctx, spanKey{}, true)
}

func fakeTraceAttrs(ctx context.Context) []slog.Attr {
	if ctx.Value(spanKey{}) == nil {
		return nil
	}
	return []slog.Attr{slog.String(log.KeyTraceID, testTraceID), slog.String(log.KeySpanID, testSpanID)}
}

func TestNewHandlerWritesJSONWithIdentitySourceAndTrace(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{
		Output: &out, Service: "echopoint", Environment: "production", Version: "1.2.3",
		ContextAttrs: fakeTraceAttrs,
	})

	ctx := log.WithAttrs(withSpan(t.Context()), slog.String(log.KeyOrgID, "org_1"))
	logger.InfoContext(ctx, "flow created", slog.String("flow_id", "f1"))

	line := onlyLine(t, out.String())
	want := map[string]any{
		"msg": "flow created", "level": "INFO", "flow_id": "f1", "org_id": "org_1",
		"service": "echopoint", "env": "production", "version": "1.2.3",
		"trace_id": testTraceID, "span_id": testSpanID,
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	source, _ := line["source"].(map[string]any)
	if file, _ := source["file"].(string); !strings.HasSuffix(file, "handler_test.go") {
		t.Errorf("source = %v, want this file", line["source"])
	}
}

func TestNewHandlerHonoursLevel(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	level := new(slog.LevelVar)
	level.Set(slog.LevelWarn)
	logger := log.New(log.Config{Output: &out, Level: level})

	logger.InfoContext(t.Context(), "dropped")
	level.Set(slog.LevelInfo)
	logger.InfoContext(t.Context(), "kept")

	if line := onlyLine(t, out.String()); line["msg"] != "kept" {
		t.Fatalf("got %v", line)
	}
}

func TestContextAttrsStayTopLevelUnderGroups(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out}).With("before", 1).WithGroup("http").With("after", 2)

	ctx := log.WithAttrs(t.Context(), slog.String(log.KeyRequestID, "req_1"))
	logger.InfoContext(ctx, "grouped", slog.Int("status", 200))

	line := onlyLine(t, out.String())
	if line["request_id"] != "req_1" || line["before"] != float64(1) {
		t.Fatalf("top level = %v", line)
	}
	group, _ := line["http"].(map[string]any)
	if group["after"] != float64(2) || group["status"] != float64(200) || group["request_id"] != nil {
		t.Fatalf("http group = %v", group)
	}
}

func TestCallSiteKeyWinsOverContextKey(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out})

	ctx := log.WithAttrs(t.Context(), slog.String(log.KeyOrgID, "from_context"))
	logger.InfoContext(ctx, "override", slog.String(log.KeyOrgID, "from_call"))

	got := out.String()
	if strings.Count(got, `"org_id"`) != 1 || !strings.Contains(got, `"org_id":"from_call"`) {
		t.Fatalf("got %s", got)
	}
}

// TestLoggerKeyWinsOverContextKey pins that a key set with Logger.With is
// written once, with the logger's value, when the context carries the same
// key: a component logger keeps its component inside a cron that puts its own
// on the context. A key set after WithGroup sits in the group and does not
// hide the context's top-level key.
func TestLoggerKeyWinsOverContextKey(t *testing.T) {
	t.Parallel()
	for _, wire := range []log.Wire{log.WireSlog, log.WireLegacy} {
		var out syncBuffer
		logger := log.New(log.Config{Output: &out, Wire: wire, ContextAttrs: fakeTraceAttrs}).
			With(slog.String(log.KeyComponent, "repository"), slog.Group("", slog.String(log.KeyTraceID, "logger")))

		ctx := log.WithScope(withSpan(t.Context()))
		log.SetAttrs(ctx, slog.String(log.KeyRequestID, "req_1"), slog.String(log.KeyComponent, "cron"))
		logger.InfoContext(ctx, "plain")
		logger.WithGroup("g").With(slog.String(log.KeyRequestID, "grouped")).InfoContext(ctx, "grouped")

		lines := jsonLines(t, out.String())
		if len(lines) != 2 {
			t.Fatalf("%v: want 2 lines, got %s", wire, out.String())
		}
		for i, raw := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if strings.Count(raw, `"component"`) != 1 || strings.Count(raw, `"trace_id"`) != 1 {
				t.Errorf("%v: line %d writes a key twice: %s", wire, i, raw)
			}
			line := lines[i]
			if line[log.KeyComponent] != "repository" || line[log.KeyTraceID] != "logger" ||
				line[log.KeyRequestID] != "req_1" || line[log.KeySpanID] != testSpanID {
				t.Errorf("%v: line %d = %v", wire, i, line)
			}
		}
		if group, _ := lines[1]["g"].(map[string]any); group[log.KeyRequestID] != "grouped" {
			t.Errorf("%v: g = %v", wire, lines[1]["g"])
		}
	}
}

// TestLoggerKeyDoesNotChangeTheScope checks that dropping a context key for
// a logger does not write into the scope that other lines read.
func TestLoggerKeyDoesNotChangeTheScope(t *testing.T) {
	t.Parallel()
	// log.Wrap without contextAttrs hands the handler a view of the scope, not
	// a copy, so dropping the logger's key must not write into it.
	for name, newLogger := range map[string]func(*syncBuffer) *slog.Logger{
		"New": func(out *syncBuffer) *slog.Logger { return log.New(log.Config{Output: out}) },
		"Wrap": func(out *syncBuffer) *slog.Logger {
			return slog.New(log.Wrap(slog.NewJSONHandler(out, nil)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out syncBuffer
			base := newLogger(&out)
			ctx := log.WithScope(t.Context())
			log.SetAttrs(ctx, slog.String(log.KeyComponent, "cron"), slog.String(log.KeyRequestID, "req_1"))

			base.With(slog.String(log.KeyComponent, "repository")).InfoContext(ctx, "first")

			want := []slog.Attr{slog.String(log.KeyComponent, "cron"), slog.String(log.KeyRequestID, "req_1")}
			if got := log.Attrs(ctx); !slices.EqualFunc(got, want, slog.Attr.Equal) {
				t.Fatalf("scope after the first line = %v, want %v", got, want)
			}
			base.InfoContext(ctx, "second")
			lines := jsonLines(t, out.String())
			if lines[1][log.KeyComponent] != "cron" || lines[1][log.KeyRequestID] != "req_1" {
				t.Fatalf("second line = %v", lines[1])
			}
		})
	}
}

func TestExportGetsContextAttrsButNotTraceOrIdentity(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	_, export := logtest.New()
	stdout := log.New(log.Config{
		Output: &out, Service: "echopoint", ContextAttrs: fakeTraceAttrs, Export: exportOnly{export.Handler()},
	})

	ctx := log.WithAttrs(withSpan(t.Context()), slog.String(log.KeyUserID, "u1"))
	stdout.WarnContext(ctx, "fan out", slog.String("password", "hunter2"))

	line := onlyLine(t, out.String())
	if line["trace_id"] == nil || line["service"] != "echopoint" || line["password"] != secret.Redacted {
		t.Fatalf("stdout line = %v", line)
	}
	entry := export.AssertLogged(t, slog.LevelWarn, "fan out",
		slog.String(log.KeyUserID, "u1"), slog.String("password", secret.Redacted))
	for _, key := range []string{"service", log.KeyTraceID, log.KeySpanID} {
		if _, ok := entry.Value(key); ok {
			t.Errorf("export entry has %s: %s", key, entry.Format())
		}
	}
}

// exportOnly hides the context from logtest's own Wrap, which would add the
// context attributes again, so the test sees exactly what NewHandler passes to
// Export.
type exportOnly struct{ slog.Handler }

func (e exportOnly) Handle(_ context.Context, r slog.Record) error {
	return e.Handler.Handle(context.Background(), r)
}

func (e exportOnly) WithAttrs(attrs []slog.Attr) slog.Handler {
	return exportOnly{e.Handler.WithAttrs(attrs)}
}

func (e exportOnly) WithGroup(name string) slog.Handler { return exportOnly{e.Handler.WithGroup(name)} }

func TestTextFormatIsCompact(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out, Format: log.FormatText})

	logger.InfoContext(log.WithAttrs(t.Context(), slog.String(log.KeyRequestID, "r1")), "hello", "n", 1)

	got := out.String()
	for _, want := range []string{"level=INFO", `msg=hello`, "source=log/handler_test.go:", "request_id=r1", "n=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}

func TestWrapConformsToSlogtest(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	newHandler := func(*testing.T) slog.Handler {
		out = syncBuffer{}
		return log.Wrap(slog.NewJSONHandler(&out, nil))
	}
	result := func(t *testing.T) map[string]any { return onlyLine(t, out.String()) }
	slogtest.Run(t, newHandler, result)
}

func TestNewHandlerConformsToSlogtest(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	newHandler := func(*testing.T) slog.Handler {
		out = syncBuffer{}
		return log.NewHandler(log.Config{Output: &out, Level: slog.LevelDebug})
	}
	result := func(t *testing.T) map[string]any {
		line := onlyLine(t, out.String())
		delete(line, slog.SourceKey) // slogtest does not expect AddSource
		return line
	}
	slogtest.Run(t, newHandler, result)
}

func TestConcurrentLoggingWithScope(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out}).With(slog.String(log.KeyComponent, "ticker"))
	ctx := log.WithScope(t.Context())
	log.SetAttrs(ctx, slog.String(log.KeyRequestID, "req_1"))

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			// The scope is shared, so the worker on a line may be another
			// goroutine's; i comes from this goroutine's own context.
			log.SetAttrs(ctx, slog.Int("worker", i))
			logger.InfoContext(log.WithAttrs(ctx, slog.Int("i", i)), "tick")
		})
	}
	wg.Wait()

	raw := out.String()
	lines := jsonLines(t, raw)
	if len(lines) != 20 {
		t.Fatalf("got %d lines", len(lines))
	}
	seen := make(map[float64]bool)
	n := 0
	for line := range strings.Lines(raw) {
		// json.Unmarshal keeps the last of duplicate keys, so count raw keys.
		for _, key := range []string{`"worker":`, `"i":`, `"request_id":`, `"component":`} {
			if c := strings.Count(line, key); c != 1 {
				t.Errorf("%s appears %d times: %s", key, c, line)
			}
		}
		decoded := lines[n]
		n++
		i, _ := decoded["i"].(float64)
		seen[i] = true
		if worker, ok := decoded["worker"].(float64); !ok || worker < 0 || worker > 19 {
			t.Errorf("worker = %v: %s", decoded["worker"], line)
		}
		if decoded["request_id"] != "req_1" || decoded["component"] != "ticker" {
			t.Errorf("lost scope or logger attrs: %s", line)
		}
	}
	if len(seen) != 20 {
		t.Fatalf("i values = %v, want 0..19 once each", seen)
	}
}

func TestWrapAddsContextAttrsExtractors(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := slog.New(log.Wrap(slog.NewJSONHandler(&out, nil), fakeTraceAttrs))

	logger.InfoContext(log.WithAttrs(withSpan(t.Context()), slog.String(log.KeyOrgID, "org_1")), "wrapped")

	line := onlyLine(t, out.String())
	if line["trace_id"] != testTraceID || line["span_id"] != testSpanID || line["org_id"] != "org_1" {
		t.Fatalf("line = %v", line)
	}
}
