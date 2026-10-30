package tracing_test

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

const incomingTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func newServer(t *testing.T) (http.Handler, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	router := chi.NewRouter()
	router.Use(tracing.RouteMiddleware)
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Get("/flows/{flowID}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Route("/orgs/{orgID}", func(r chi.Router) {
		r.Get("/members/{memberID}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	return tracing.Middleware(otelhttp.WithTracerProvider(provider))(router), recorder
}

func serve(handler http.Handler, method, target string, header http.Header) {
	request := httptest.NewRequest(method, target, nil)
	maps.Copy(request.Header, header)
	handler.ServeHTTP(httptest.NewRecorder(), request)
}

func TestSpanNamedAfterRoutePattern(t *testing.T) {
	cases := map[string]string{
		"/flows/flow_2abc":           "GET /flows/{flowID}",
		"/orgs/org_1/members/user_2": "GET /orgs/{orgID}/members/{memberID}",
		"/missing/123":               "GET",
	}
	for target, want := range cases {
		t.Run(target, func(t *testing.T) {
			handler, recorder := newServer(t)
			serve(handler, http.MethodGet, target, nil)
			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("got %d spans, want 1", len(spans))
			}
			if spans[0].Name() != want {
				t.Fatalf("span name = %q, want %q", spans[0].Name(), want)
			}
			if spans[0].SpanKind() != trace.SpanKindServer {
				t.Fatalf("span kind = %v", spans[0].SpanKind())
			}
		})
	}
}

func TestRouteAttributeRecorded(t *testing.T) {
	handler, recorder := newServer(t)
	serve(handler, http.MethodGet, "/flows/flow_2abc", nil)
	for _, kv := range recorder.Ended()[0].Attributes() {
		if kv.Key == "http.route" {
			if kv.Value.AsString() != "/flows/{flowID}" {
				t.Fatalf("http.route = %q", kv.Value.AsString())
			}
			return
		}
	}
	t.Fatal("http.route attribute missing")
}

func TestServerSpanContinuesIncomingTraceparent(t *testing.T) {
	handler, recorder := newServer(t)
	serve(handler, http.MethodGet, "/flows/flow_2abc", http.Header{"Traceparent": {incomingTraceparent}})
	span := recorder.Ended()[0]
	if got := span.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %s, want the edge's", got)
	}
	if got := span.Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Fatalf("parent span id = %s", got)
	}
}

func TestHealthIsNotTraced(t *testing.T) {
	handler, recorder := newServer(t)
	serve(handler, http.MethodGet, "/health", nil)
	if spans := recorder.Ended(); len(spans) != 0 {
		t.Fatalf("health check produced %d spans", len(spans))
	}
}

func TestClientPropagatesTraceContext(t *testing.T) {
	var gotTraceparent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, parent := provider.Tracer("test").Start(context.Background(), "parent")

	client := tracing.Client(upstream.Client(), otelhttp.WithTracerProvider(provider))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+"/v1/licenses", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	parent.End()

	wantPrefix := "00-" + parent.SpanContext().TraceID().String() + "-"
	if len(gotTraceparent) < len(wantPrefix) || gotTraceparent[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("traceparent = %q, want trace %s", gotTraceparent, parent.SpanContext().TraceID())
	}
	var clientSpans int
	for _, span := range recorder.Ended() {
		if span.SpanKind() == trace.SpanKindClient {
			clientSpans++
		}
	}
	if clientSpans != 1 {
		t.Fatalf("got %d client spans, want 1", clientSpans)
	}
}

func TestClientDoesNotMutateBase(t *testing.T) {
	base := &http.Client{}
	client := tracing.Client(base)
	if base.Transport != nil {
		t.Fatal("Client mutated the base client")
	}
	if client.Transport == nil {
		t.Fatal("Client returned no transport")
	}
	if tracing.Client(nil).Transport == nil {
		t.Fatal("Client(nil) returned no transport")
	}
}
