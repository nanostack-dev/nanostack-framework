package log_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// The redaction benchmarks measure what core/log adds to one log line over
// the bare slog JSON handler it writes with. Every row writes JSON with
// source locations to io.Discard at Info, on a context that carries a request
// scope of three attributes (as log.HTTPMiddleware opens):
//
//   - handler=bare is slog.NewJSONHandler alone: the floor. The scope is not
//     read, so its line has three attributes fewer.
//   - handler=wrap is log.Wrap of the same handler: the context stage and the
//     redaction stage.
//   - handler=new is log.New, the process handler: Wrap plus the service
//     identity.
//
// attrs=clean logs eight plain attributes, the shape of a request summary.
// attrs=secret-key adds an attribute whose key secret.IsKey matches and a
// secret.Secret. attrs=header adds an http.Header, which the redaction stage
// turns into a group so its Authorization entry is replaced.

func benchScope() context.Context {
	ctx := log.WithScope(context.Background())
	log.SetAttrs(ctx,
		slog.String(log.KeyRequestID, "01HZX3V6Q8K2J7M4N5P6R7S8T9"),
		slog.String(log.KeyMethod, http.MethodGet),
		slog.String(log.KeyPath, "/v1/flows/flw_123/runs"),
	)
	return ctx
}

func benchCleanAttrs() []slog.Attr {
	return []slog.Attr{
		slog.String(log.KeyRoute, "GET /v1/flows/{id}/runs"),
		slog.Int(log.KeyStatus, http.StatusOK),
		slog.Int(log.KeyBytesOut, 512),
		slog.Duration(log.KeyDuration, 1250*time.Microsecond),
		slog.String(log.KeyClientIP, "203.0.113.9"),
		slog.String(log.KeyUserAgent, "echopoint-cli/1.4.0"),
		slog.String(log.KeyOrgID, "org_42"),
		slog.String(log.KeyUserID, "usr_7"),
	}
}

func BenchmarkRedaction(b *testing.B) {
	opts := &slog.HandlerOptions{AddSource: true}
	handlers := []struct {
		name   string
		logger *slog.Logger
	}{
		{"handler=bare", slog.New(slog.NewJSONHandler(io.Discard, opts))},
		{"handler=wrap", slog.New(log.Wrap(slog.NewJSONHandler(io.Discard, opts)))},
		{"handler=new", log.New(log.Config{Output: io.Discard, Service: "bench", Environment: "production"})},
	}
	header := http.Header{
		"Authorization": {"Bearer tok_0123456789abcdef"},
		"Accept":        {"application/json"},
		"User-Agent":    {"echopoint-cli/1.4.0"},
	}
	cases := []struct {
		name  string
		attrs []slog.Attr
	}{
		{"attrs=clean", benchCleanAttrs()},
		{"attrs=secret-key", append(benchCleanAttrs(),
			slog.String("api_key", "sk_live_0123456789"),
			slog.Any("db_password", secret.New("pw")),
		)},
		{"attrs=header", append(benchCleanAttrs(), slog.Any("headers", header))},
	}
	ctx := benchScope()
	for _, h := range handlers {
		for _, c := range cases {
			b.Run(h.name+"/"+c.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					h.logger.LogAttrs(ctx, slog.LevelInfo, "http request", c.attrs...)
				}
			})
		}
	}
}
