package tracing

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/pkg/health"
)

const serverOperation = "http.server"

// Middleware returns the inbound HTTP instrumentation. Install it outermost,
// around the whole router, so the server span — continuing an incoming W3C
// traceparent — exists before any logging middleware reads the context.
//
// The span is first named after the method only; RouteMiddleware renames it
// to "METHOD /route/{pattern}" once chi has matched a route, so raw paths with
// identifiers never become span names. Requests to health.DefaultPath are not
// traced. opts are appended to the defaults, so a caller can add filters or
// pass a TracerProvider.
func Middleware(opts ...otelhttp.Option) func(http.Handler) http.Handler {
	defaults := []otelhttp.Option{
		otelhttp.WithPropagators(Propagator()),
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != health.DefaultPath }),
	}
	return otelhttp.NewMiddleware(serverOperation, append(defaults, opts...)...)
}

// RouteMiddleware names the active server span after the matched chi route
// pattern and records it as http.route. Register it first with
// (*chi.Mux).Use on the root router; it reads the pattern after the request is
// served, so mounted sub-routers contribute their full pattern. A request that
// matched no route keeps the method-only name.
func RouteMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		span := trace.SpanFromContext(r.Context())
		if !span.IsRecording() {
			return
		}
		routeContext := chi.RouteContext(r.Context())
		if routeContext == nil {
			return
		}
		pattern := routeContext.RoutePattern()
		if pattern == "" {
			return
		}
		span.SetName(spanMethod(r.Method) + " " + pattern)
		span.SetAttributes(attribute.String("http.route", pattern))
		if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
			labeler.Add(attribute.String("http.route", pattern))
		}
	})
}

func spanMethod(method string) string {
	switch method = strings.ToUpper(method); method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "HTTP"
	}
}

// Transport wraps base (http.DefaultTransport when nil) so each outbound
// request gets a client span and carries the caller's trace context in a W3C
// traceparent header. Use it for service-to-service clients.
func Transport(base http.RoundTripper, opts ...otelhttp.Option) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	defaults := []otelhttp.Option{otelhttp.WithPropagators(Propagator())}
	return otelhttp.NewTransport(base, append(defaults, opts...)...)
}

// Client returns a shallow copy of base (a zero http.Client when nil) whose
// transport is wrapped by Transport. Outbound requests must be built with the
// caller's context (http.NewRequestWithContext) to join its trace.
func Client(base *http.Client, opts ...otelhttp.Option) *http.Client {
	client := &http.Client{}
	if base != nil {
		copied := *base
		client = &copied
	}
	client.Transport = Transport(client.Transport, opts...)
	return client
}
