package httpserver_test

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog" //nolint:depguard // the v0.22 baseline: the zerolog path services run today

	corefault "github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
	"github.com/nanostack-dev/nanostack-framework/modules/httpserver"
	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope"
)

// The error path benchmarks compare the writers a strict server's error hooks
// call:
//
//   - impl=legacy is modules/httpserver.StrictErrorHandler, which logs through
//     zerolog, on a request that carries the request-scoped logger of
//     requestlog.Contextualize.
//   - impl=beta is errorenvelope.Writer, the same envelope logged through
//     core/log, on a request that carries the scope of log.HTTPMiddleware.
//   - impl=beta-problem is problem.Write, the RFC 9457 default of a new
//     service (a different wire contract, shown for scale: its validation
//     answer is a 422, and its 404 comes from a core/fault kind).
//   - impl=floor is pkg/fault.WriteJSON of the same envelope: no resolution
//     and no log line. It is the floor, not a peer.
//
// Cases: a handler returning a 404 fault (ResponseError), the validation 400
// the generated server passes to RequestError, and an unmodelled handler
// error (a 500 with the error's type and verbose form logged). Loggers write
// JSON to io.Discard at the level the row names; production runs at info.
// At info the beta Writer skips the Debug line of a 4xx while
// StrictErrorHandler writes an Info line for a modelled response error, so
// the level=debug rows compare the two with both lines written.
//
// The file sits in the legacy tree because only a legacy package may import
// both sides (internal/layers).

type benchResponseWriter struct {
	header http.Header
	status int
}

func (w *benchResponseWriter) Header() http.Header         { return w.header }
func (w *benchResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *benchResponseWriter) WriteHeader(status int)      { w.status = status }

func benchErrNotFound() error {
	return fault.NotFound("FLOW_NOT_FOUND", "Flow flw_123 does not exist.")
}

// benchKindNotFound is the 404 a beta handler returns to problem.Write: a
// core/fault kind. problem.Write maps a pkg/fault error, which carries no
// problem type, to a 500.
func benchKindNotFound() error {
	return corefault.NewKind("flow.not_found", http.StatusNotFound, "Flow not found",
		"https://errors.nanostack.dev/flow-not-found")
}

func benchErrValidation() error {
	return problem.Validation(validate.Errors{
		{Pointer: "/name", Code: validate.CodeMinLength, Detail: "must be at least 1 character long"},
		{Pointer: "/url", Code: validate.CodeFormat, Detail: "must be an absolute URI"},
	})
}

func benchErrUnmodelled() error {
	return fmt.Errorf("load flow flw_123: %w", errors.New("pq: connection refused"))
}

func betaRequest(lvl slog.Level) (*http.Request, *slog.Logger) {
	logger := log.New(log.Config{Output: io.Discard, Level: lvl, Service: "bench", Environment: "production"})
	r := httptest.NewRequest(http.MethodGet, "/v1/flows/flw_123", http.NoBody)
	ctx := log.WithScope(r.Context())
	log.SetAttrs(ctx,
		slog.String(log.KeyRequestID, "01HZX3V6Q8K2J7M4N5P6R7S8T9"),
		slog.String(log.KeyMethod, r.Method),
		slog.String(log.KeyPath, r.URL.Path),
	)
	return r.WithContext(ctx), logger
}

func legacyRequest(lvl zerolog.Level) (*http.Request, zerolog.Logger) {
	logger := zerolog.New(io.Discard).Level(lvl).With().Timestamp().Caller().Logger()
	r := httptest.NewRequest(http.MethodGet, "/v1/flows/flw_123", http.NoBody)
	bound := logger.With().
		Str("request_id", "01HZX3V6Q8K2J7M4N5P6R7S8T9").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Logger()
	return r.WithContext(bound.WithContext(r.Context())), logger
}

type errorWriter struct {
	request  func(http.ResponseWriter, *http.Request, error)
	response func(http.ResponseWriter, *http.Request, error)
	r        *http.Request
	restore  func() // undoes the writer's setup; nil when there is none
}

func BenchmarkErrorPath(b *testing.B) {
	writers := map[string]func(level string) errorWriter{
		"impl=legacy": func(level string) errorWriter {
			lvl := zerolog.InfoLevel
			if level == "debug" {
				lvl = zerolog.DebugLevel
			}
			r, logger := legacyRequest(lvl)
			h := httpserver.NewStrictErrorHandler(httpserver.StrictErrorHandlerOptions{Logger: logger})
			return errorWriter{request: h.HandleRequestError, response: h.HandleResponseError, r: r}
		},
		"impl=beta": func(level string) errorWriter {
			lvl := slog.LevelInfo
			if level == "debug" {
				lvl = slog.LevelDebug
			}
			r, logger := betaRequest(lvl)
			envelope := errorenvelope.New(errorenvelope.Options{Logger: logger})
			return errorWriter{request: envelope.RequestError, response: envelope.ResponseError, r: r}
		},
		"impl=beta-problem": func(level string) errorWriter {
			lvl := slog.LevelInfo
			if level == "debug" {
				lvl = slog.LevelDebug
			}
			r, logger := betaRequest(lvl)
			previous := slog.Default()
			slog.SetDefault(logger) // problem.Write logs through slog.Default
			return errorWriter{
				request:  problem.Write,
				response: problem.Write,
				r:        r,
				restore:  func() { slog.SetDefault(previous) },
			}
		},
		"impl=floor": func(string) errorWriter {
			r := httptest.NewRequest(http.MethodGet, "/v1/flows/flw_123", http.NoBody)
			write := func(w http.ResponseWriter, _ *http.Request, err error) {
				apiErr, ok := fault.As(err)
				if !ok {
					apiErr = fault.ErrUnexpected
				}
				fault.WriteJSON(w, apiErr)
			}
			return errorWriter{request: write, response: write, r: r}
		},
	}
	cases := []struct {
		name    string
		err     func() error
		request bool // the request hook, not the response hook
		want    int
		problem int // the status problem.Write answers
	}{
		{"case=404", benchErrNotFound, false, http.StatusNotFound, http.StatusNotFound},
		{"case=400-validation", benchErrValidation, true, http.StatusBadRequest, http.StatusUnprocessableEntity},
		{"case=500-unmodelled", benchErrUnmodelled, false, http.StatusInternalServerError,
			http.StatusInternalServerError},
	}
	for _, level := range []string{"info", "debug"} {
		for _, impl := range []string{"impl=legacy", "impl=beta", "impl=beta-problem", "impl=floor"} {
			for _, c := range cases {
				if impl == "impl=floor" && (level == "debug" || c.request) {
					continue // the floor neither logs nor maps a problem
				}
				b.Run(impl+"/"+c.name+"/level="+level, func(b *testing.B) {
					ew := writers[impl](level)
					if ew.restore != nil {
						b.Cleanup(ew.restore)
					}
					hook := ew.response
					if c.request {
						hook = ew.request
					}
					err, want := c.err(), c.want
					if impl == "impl=beta-problem" {
						want = c.problem
						if c.name == "case=404" {
							err = benchKindNotFound()
						}
					}
					w := &benchResponseWriter{header: make(http.Header)}
					b.ReportAllocs()
					for b.Loop() {
						hook(w, ew.r, err)
					}
					if w.status != want {
						b.Fatalf("status = %d, want %d", w.status, want)
					}
				})
			}
		}
	}
}
