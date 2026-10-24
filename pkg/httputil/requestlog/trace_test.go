package requestlog_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/requestlog"
	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const edgeTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// lines collects every log line, so a test can inspect the raw bytes:
// json.Unmarshal keeps only the last of a duplicated key.
type lines struct{ buf bytes.Buffer }

func (l *lines) Write(p []byte) (int, error) { return l.buf.Write(p) }

func (l *lines) all() []string {
	return strings.Split(strings.TrimSpace(l.buf.String()), "\n")
}

// tracedRequest serves one request through tracing.Middleware →
// Contextualize → handler, with handler logging both through the request
// logger and through a Binder-rebuilt logger, then the access log.
func tracedRequest(t *testing.T, provider trace.TracerProvider) (*lines, trace.SpanContext) {
	t.Helper()
	sink := &lines{}
	base := zerolog.New(sink)
	binder := log.NewBinder(base, requestlog.NewLogEnricher(), log.TraceEnricher())

	var serverSpan trace.SpanContext
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverSpan = trace.SpanContextFromContext(r.Context())
		requestlog.From(r.Context()).Info().Msg("request logger")
		log.Ctx(binder.Bind(r.Context())).Info().Msg("bound logger")
		w.WriteHeader(http.StatusOK)
	})
	chain := tracing.Middleware(otelhttp.WithTracerProvider(provider))(
		requestlog.Contextualize(base)(requestlog.New(base, requestlog.Options{})(handler)),
	)

	request := httptest.NewRequest(http.MethodGet, "/flows/abc", nil)
	request.Header.Set("traceparent", edgeTraceparent)
	chain.ServeHTTP(httptest.NewRecorder(), request)
	return sink, serverSpan
}

func TestLogLinesCarryTraceIDsExactlyOnce(t *testing.T) {
	provider := sdktrace.NewTracerProvider()
	sink, serverSpan := tracedRequest(t, provider)

	got := sink.all()
	if len(got) != 3 {
		t.Fatalf("got %d log lines, want 3: %v", len(got), got)
	}
	for _, raw := range got {
		for _, key := range []string{log.TraceIDKey, log.SpanIDKey} {
			if count := strings.Count(raw, `"`+key+`"`); count != 1 {
				t.Fatalf("%s appeared %d times in %s", key, count, raw)
			}
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatal(err)
		}
		if line[log.TraceIDKey] != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("trace_id = %v in %s, want the edge's", line[log.TraceIDKey], raw)
		}
		if line[log.SpanIDKey] != serverSpan.SpanID().String() {
			t.Fatalf("span_id = %v, want the server span %s", line[log.SpanIDKey], serverSpan.SpanID())
		}
	}
}

// With tracing disabled (no-op provider) the edge's trace id still reaches the
// logs, so a log row links to the trace traefik recorded.
func TestNoOpProviderKeepsEdgeTraceID(t *testing.T) {
	sink, _ := tracedRequest(t, noop.NewTracerProvider())
	for _, raw := range sink.all() {
		if !strings.Contains(raw, `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`) {
			t.Fatalf("missing edge trace id in %s", raw)
		}
	}
}

func TestContextualizeWithoutSpanAddsNoTraceFields(t *testing.T) {
	sink := &lines{}
	base := zerolog.New(sink)
	handler := requestlog.Contextualize(base)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestlog.From(r.Context()).Info().Msg("untraced")
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/flows", nil))
	if strings.Contains(sink.buf.String(), log.TraceIDKey) {
		t.Fatalf("unexpected trace fields: %s", sink.buf.String())
	}
}
