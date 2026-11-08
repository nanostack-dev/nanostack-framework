package log_test

import (
	"log/slog"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
)

func keys(attrs []slog.Attr) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value.String()
	}
	return m
}

func TestWithAttrsReplacesSameKey(t *testing.T) {
	t.Parallel()
	ctx := log.WithAttrs(t.Context(), slog.String(log.KeyOrgID, "a"), slog.String(log.KeyComponent, "cron"))
	ctx = log.WithAttrs(ctx, slog.String(log.KeyOrgID, "b"))

	attrs := log.Attrs(ctx)
	if len(attrs) != 2 || keys(attrs)[log.KeyOrgID] != "b" {
		t.Fatalf("attrs = %v", attrs)
	}
}

func TestWithAttrsDoesNotLeakIntoSiblings(t *testing.T) {
	t.Parallel()
	parent := log.WithAttrs(t.Context(), slog.String(log.KeyComponent, "cron"))
	first := log.WithAttrs(parent, slog.String(log.KeyOrgID, "org_1"))
	second := log.WithAttrs(parent, slog.String(log.KeyOrgID, "org_2"))

	if keys(log.Attrs(first))[log.KeyOrgID] != "org_1" || keys(log.Attrs(second))[log.KeyOrgID] != "org_2" {
		t.Fatal("sibling contexts share attributes")
	}
	if _, ok := keys(log.Attrs(parent))[log.KeyOrgID]; ok {
		t.Fatal("child attribute leaked into parent")
	}
}

func TestSetAttrsIsVisibleThroughEveryDerivedContext(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	outer := log.WithScope(t.Context())
	inner := log.WithAttrs(outer, slog.String("step", "auth"))

	if !log.SetAttrs(inner, slog.String(log.KeyOrgID, "org_1")) {
		t.Fatal("SetAttrs found no scope")
	}
	logger.InfoContext(outer, "summary")

	rec.AssertLogged(t, slog.LevelInfo, "summary", slog.String(log.KeyOrgID, "org_1"))
}

func TestSetAttrsWithoutScopeReportsFalse(t *testing.T) {
	t.Parallel()
	if log.SetAttrs(t.Context(), slog.String(log.KeyOrgID, "x")) {
		t.Fatal("SetAttrs reported a scope on a bare context")
	}
}

func TestWithScopeCopiesParent(t *testing.T) {
	t.Parallel()
	parent := log.WithScope(t.Context())
	log.SetAttrs(parent, slog.String(log.KeyRequestID, "r1"))
	child := log.WithScope(parent)
	log.SetAttrs(child, slog.String(log.KeyOrgID, "org_1"))

	if got := keys(log.Attrs(child)); got[log.KeyRequestID] != "r1" || got[log.KeyOrgID] != "org_1" {
		t.Fatalf("child = %v", got)
	}
	if _, ok := keys(log.Attrs(parent))[log.KeyOrgID]; ok {
		t.Fatal("child scope wrote into parent")
	}
}

func TestWithAttrsWinsOverScope(t *testing.T) {
	t.Parallel()
	ctx := log.WithScope(t.Context())
	ctx = log.WithAttrs(ctx, slog.String(log.KeyOrgID, "branch"))
	log.SetAttrs(ctx, slog.String(log.KeyOrgID, "scope"))

	if got := keys(log.Attrs(ctx))[log.KeyOrgID]; got != "branch" {
		t.Fatalf("org_id = %q, want branch", got)
	}
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	if got := log.RequestID(t.Context()); got != "" {
		t.Fatalf("bare context: %q", got)
	}
	ctx := log.WithScope(t.Context())
	log.SetAttrs(ctx, slog.String(log.KeyRequestID, "r1"))
	if got := log.RequestID(ctx); got != "r1" {
		t.Fatalf("got %q", got)
	}
}
