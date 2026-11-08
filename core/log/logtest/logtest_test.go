package logtest_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

func TestRecorderFlattensGroupsAndMatchesValues(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	err := errors.New("boom")

	logger.With("component", "worker").WithGroup("job").InfoContext(t.Context(), "done",
		slog.Int64("id", 7), slog.Group("timing", slog.Duration("took", time.Second)),
		slog.Any("tags", []string{"a", "b"}), log.Err(fmt.Errorf("run: %w", err)))

	entry := rec.AssertLogged(t, slog.LevelInfo, "done",
		slog.String("component", "worker"),
		slog.Int64("job.id", 7),
		slog.Group("job", slog.Group("timing", slog.Duration("took", time.Second))),
		slog.Any("job.tags", []string{"a", "b"}),
		slog.Any("job.error", err), // matches the wrapped error through errors.Is
	)
	if src := entry.Source(); src == nil || !strings.HasSuffix(src.File, "logtest_test.go") {
		t.Fatalf("source = %+v", src)
	}
	if len(rec.Find(slog.LevelInfo, "done", slog.Int64("job.id", 8))) != 0 {
		t.Fatal("a different value matched")
	}
}

func TestRecorderAppliesContextAttrsAndRedaction(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	ctx := log.WithAttrs(t.Context(), slog.String(log.KeyOrgID, "org_1"))

	logger.WarnContext(ctx, "login failed", slog.String("password", "hunter2"))

	rec.AssertLogged(t, slog.LevelWarn, "login failed",
		slog.String(log.KeyOrgID, "org_1"), slog.String("password", secret.Redacted))
}

// fakeT records a failure instead of stopping the test.
type fakeT struct {
	testing.TB
	failure string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatalf(format string, args ...any) { f.failure = fmt.Sprintf(format, args...) }

func TestAssertionsReportFailures(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	logger.InfoContext(t.Context(), "twice")
	logger.InfoContext(t.Context(), "twice")

	ft := &fakeT{TB: t}
	rec.AssertLogged(ft, slog.LevelInfo, "missing")
	if !strings.Contains(ft.failure, `found 0`) || !strings.Contains(ft.failure, `"twice"`) {
		t.Fatalf("missing entry: %q", ft.failure)
	}
	ft.failure = ""
	rec.AssertLogged(ft, slog.LevelInfo, "twice")
	if !strings.Contains(ft.failure, "found 2") {
		t.Fatalf("duplicate entry: %q", ft.failure)
	}
	ft.failure = ""
	rec.AssertNotLogged(ft, "twice")
	if ft.failure == "" {
		t.Fatal("AssertNotLogged passed on a logged message")
	}
	rec.Reset()
	if len(rec.Entries()) != 0 {
		t.Fatal("Reset kept entries")
	}
}

// TestCaptureIsolatesParallelTests runs package-level slog calls from many
// parallel subtests; each sees only its own line.
func TestCaptureIsolatesParallelTests(t *testing.T) {
	t.Parallel()
	for i := range 8 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			ctx, rec := logtest.Capture(t)
			ctx = log.WithAttrs(ctx, slog.Int("worker", i))

			slog.InfoContext(ctx, "tick", "n", i)
			slog.Default().With("component", "c").WarnContext(ctx, "grouped")

			if got := len(rec.Entries()); got != 2 {
				t.Fatalf("got %d entries, want 2", got)
			}
			rec.AssertLogged(t, slog.LevelInfo, "tick", slog.Int("n", i), slog.Int("worker", i))
			rec.AssertLogged(t, slog.LevelWarn, "grouped", slog.String("component", "c"))
		})
	}
}

// TestRecorderInsideSynctest checks that a recorder works in a bubble with
// fake time and an in-memory HTTP server.
func TestRecorderInsideSynctest(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		logger, rec := logtest.New()
		start := time.Now()
		app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(3 * time.Second) // fake time: returns at once
			logger.InfoContext(r.Context(), "slept")
			_, _ = io.WriteString(w, "ok")
		})
		srv := httptest.NewTestServer(t, log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(app))

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://app.test/", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		summary := rec.AssertLogged(t, slog.LevelInfo, "http request", slog.Duration("duration", 3*time.Second))
		if !summary.Time.Equal(start.Add(3 * time.Second)) {
			t.Fatalf("summary time %v, want start+3s", summary.Time)
		}
		rec.AssertLogged(t, slog.LevelInfo, "slept")
	})
}

func TestRecorderContextForForeignContexts(t *testing.T) {
	t.Parallel()
	_, rec := logtest.Capture(t)
	ctx := rec.Context(context.WithoutCancel(t.Context()))

	slog.ErrorContext(ctx, "detached")

	rec.AssertLogged(t, slog.LevelError, "detached")
}
