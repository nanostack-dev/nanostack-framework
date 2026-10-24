package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/nanostack-dev/nanostack-framework/pkg/health"
	"github.com/rs/zerolog"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestServerSpansNamedByRouteAndHealthUntraced(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	var sawSpan bool
	options := Options{
		Health: health.Config{Service: "anchor"},
		ConfigureRouter: func(router *chi.Mux) {
			router.Get("/products/{productID}", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		},
		// An application middleware stands in for requestlog.Contextualize:
		// it must already see the server span.
		Middlewares: []func(http.Handler) http.Handler{func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawSpan = trace.SpanContextFromContext(r.Context()).IsValid()
				next.ServeHTTP(w, r)
			})
		}},
	}.withDefaults()
	router, err := setupRouter(options, zerolog.Nop(), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.Handler(router)
	for i := len(options.Middlewares) - 1; i >= 0; i-- {
		handler = options.Middlewares[i](handler)
	}
	handler = tracingMiddleware(provider)(handler)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/products/prd_123", nil))
	if !sawSpan {
		t.Fatal("application middleware ran before the server span existed")
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, health.DefaultPath, nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1 (health must not be traced)", len(spans))
	}
	if spans[0].Name() != "GET /products/{productID}" {
		t.Fatalf("span name = %q", spans[0].Name())
	}
}
