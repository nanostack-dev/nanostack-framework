package log_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog" //nolint:depguard // the v0.22 baseline: the zerolog path services run today

	corelog "github.com/nanostack-dev/nanostack-framework/core/log"
)

// BenchmarkLogLine compares the cost of one request-shaped log line: eight
// attributes on a logger that already carries request_id, method and path.
//
//   - impl=legacy is zerolog as modules/logging builds it (JSON, timestamp,
//     caller), with the request fields bound on a child logger, as
//     requestlog.Contextualize binds them.
//   - impl=beta is core/log's process handler (log.New), with the request
//     fields in the context scope, as log.HTTPMiddleware opens it.
//   - impl=beta-nosource is the bare slog JSON handler without source
//     locations and without the core/log stages: what slog itself costs.
//
// The file sits in the legacy tree because only a legacy package may import
// zerolog next to core/log (internal/layers).
func BenchmarkLogLine(b *testing.B) {
	b.Run("impl=legacy", func(b *testing.B) {
		logger := zerolog.New(io.Discard).Level(zerolog.InfoLevel).With().Timestamp().Caller().
			Str("request_id", "01HZX3V6Q8K2J7M4N5P6R7S8T9").
			Str("method", http.MethodGet).
			Str("path", "/v1/flows/flw_123/runs").
			Logger()
		b.ReportAllocs()
		for b.Loop() {
			logger.Info().
				Str("route", "GET /v1/flows/{id}/runs").
				Int("status", http.StatusOK).
				Int("bytes_out", 512).
				Dur("duration", 1250*time.Microsecond).
				Str("client_ip", "203.0.113.9").
				Str("user_agent", "echopoint-cli/1.4.0").
				Str("org_id", "org_42").
				Str("user_id", "usr_7").
				Msg("http request")
		}
	})

	attrs := func() []slog.Attr {
		return []slog.Attr{
			slog.String(corelog.KeyRoute, "GET /v1/flows/{id}/runs"),
			slog.Int(corelog.KeyStatus, http.StatusOK),
			slog.Int(corelog.KeyBytesOut, 512),
			slog.Duration(corelog.KeyDuration, 1250*time.Microsecond),
			slog.String(corelog.KeyClientIP, "203.0.113.9"),
			slog.String(corelog.KeyUserAgent, "echopoint-cli/1.4.0"),
			slog.String(corelog.KeyOrgID, "org_42"),
			slog.String(corelog.KeyUserID, "usr_7"),
		}
	}
	ctx := corelog.WithScope(context.Background())
	corelog.SetAttrs(ctx,
		slog.String(corelog.KeyRequestID, "01HZX3V6Q8K2J7M4N5P6R7S8T9"),
		slog.String(corelog.KeyMethod, http.MethodGet),
		slog.String(corelog.KeyPath, "/v1/flows/flw_123/runs"),
	)

	b.Run("impl=beta", func(b *testing.B) {
		logger := corelog.New(corelog.Config{Output: io.Discard})
		line := attrs()
		b.ReportAllocs()
		for b.Loop() {
			logger.LogAttrs(ctx, slog.LevelInfo, "http request", line...)
		}
	})

	b.Run("impl=beta-nosource", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil)).With(
			slog.String(corelog.KeyRequestID, "01HZX3V6Q8K2J7M4N5P6R7S8T9"),
			slog.String(corelog.KeyMethod, http.MethodGet),
			slog.String(corelog.KeyPath, "/v1/flows/flw_123/runs"),
		)
		line := attrs()
		b.ReportAllocs()
		for b.Loop() {
			logger.LogAttrs(ctx, slog.LevelInfo, "http request", line...)
		}
	})
}
