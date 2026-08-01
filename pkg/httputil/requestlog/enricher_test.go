package requestlog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/requestlog"
	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
)

// The regression this package pairs with pkg/log to prevent: binding inside a
// request must not lose the identity Contextualize established.
func TestBindKeepsRequestIdentity(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)
	binder := log.NewBinder(base, requestlog.NewLogEnricher(), log.Static("service", "echopoint"))

	var line map[string]any
	handler := requestlog.Contextualize(base)(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			ctx := binder.Bind(r.Context())
			log.Ctx(ctx).Info().Msg("handled")
			line = sink.decode(t)
		},
	))

	request := httptest.NewRequest(http.MethodGet, "/collections/abc", nil)
	request.Header.Set(requestlog.RequestIDHeader, "req_fixed")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if line["request_id"] != "req_fixed" {
		t.Fatalf("request_id = %v, want req_fixed", line["request_id"])
	}
	if line["method"] != http.MethodGet {
		t.Fatalf("method = %v", line["method"])
	}
	if line["path"] != "/collections/abc" {
		t.Fatalf("path = %v", line["path"])
	}
	if line["service"] != "echopoint" {
		t.Fatalf("service = %v", line["service"])
	}
}

// Binding twice must not duplicate keys — the property that makes deriving from
// base rather than from the context logger the right call.
func TestRebindDoesNotDuplicateRequestFields(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)
	binder := log.NewBinder(base, requestlog.NewLogEnricher())

	handler := requestlog.Contextualize(base)(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			ctx := binder.Bind(binder.Bind(r.Context()))
			log.Ctx(ctx).Info().Msg("handled")
		},
	))

	request := httptest.NewRequest(http.MethodGet, "/flows", nil)
	request.Header.Set(requestlog.RequestIDHeader, "req_fixed")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// json.Unmarshal silently keeps the last of a duplicate pair, so the raw
	// line is the only place a duplicate key is visible.
	if count := strings.Count(string(sink.last), `"request_id"`); count != 1 {
		t.Fatalf("request_id appeared %d times in %s, want exactly 1", count, sink.last)
	}
}

// Off the HTTP path the enricher contributes nothing and must not fabricate
// empty fields.
func TestEnricherIsNoOpWithoutARequest(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)

	ctx := log.NewBinder(base, requestlog.NewLogEnricher()).Bind(context.Background())
	log.Ctx(ctx).Info().Msg("cron tick")

	line := sink.decode(t)
	for _, key := range []string{"request_id", "method", "path"} {
		if _, ok := line[key]; ok {
			t.Fatalf("expected no %q off the HTTP path, got %v", key, line)
		}
	}
}

type capture struct{ last []byte }

func (c *capture) Write(p []byte) (int, error) {
	c.last = append(c.last[:0], p...)
	return len(p), nil
}

func (c *capture) decode(t *testing.T) map[string]any {
	t.Helper()
	if len(c.last) == 0 {
		t.Fatal("no log line was written")
	}
	var line map[string]any
	if err := json.Unmarshal(c.last, &line); err != nil {
		t.Fatalf("decode log line %q: %v", c.last, err)
	}
	return line
}
