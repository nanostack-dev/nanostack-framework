package requestlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/requestlog"
	"github.com/rs/zerolog"
)

func TestContextualize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		inboundID       string
		wantReuseHeader bool
	}{
		{name: "mints id when absent", inboundID: "", wantReuseHeader: false},
		{name: "reuses inbound id", inboundID: "req_inbound123", wantReuseHeader: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			base := zerolog.New(&buf)

			var (
				gotID     string
				gotLogger *zerolog.Logger
			)
			handler := requestlog.Contextualize(base)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				gotID = requestlog.RequestIDFromContext(r.Context())
				gotLogger = requestlog.From(r.Context())
				gotLogger.Info().Msg("handler log")
			}))

			req := httptest.NewRequest(http.MethodGet, "/flows", nil)
			if tt.inboundID != "" {
				req.Header.Set(requestlog.RequestIDHeader, tt.inboundID)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if gotID == "" {
				t.Fatal("expected a request id on the context")
			}
			if tt.wantReuseHeader && gotID != tt.inboundID {
				t.Fatalf("expected inbound id %q reused, got %q", tt.inboundID, gotID)
			}
			if !tt.wantReuseHeader {
				parsed, err := uuid.Parse(gotID)
				if err != nil {
					t.Fatalf("expected minted id to be a UUID, got %q: %v", gotID, err)
				}
				if parsed.Version() != 7 {
					t.Fatalf("expected minted id to be UUIDv7, got version %d", parsed.Version())
				}
			}
			if respID := rec.Header().Get(requestlog.RequestIDHeader); respID != gotID {
				t.Fatalf("response header %q does not match context id %q", respID, gotID)
			}

			fields := decodeLog(t, buf.String())
			if fields["request_id"] != gotID {
				t.Fatalf("handler log missing request_id: got %v", fields["request_id"])
			}
			if fields["method"] != http.MethodGet {
				t.Fatalf("handler log missing method: got %v", fields["method"])
			}
			if fields["path"] != "/flows" {
				t.Fatalf("handler log missing path: got %v", fields["path"])
			}
		})
	}
}

func TestUpdateContextEnrichesDownstreamLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := zerolog.New(&buf)

	handler := requestlog.Contextualize(base)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Mirrors how auth middleware enriches the request logger in place once
		// the org id is known.
		requestlog.From(r.Context()).UpdateContext(func(c zerolog.Context) zerolog.Context {
			return c.Str("org_id", "org_42")
		})
		requestlog.From(r.Context()).Info().Msg("after enrichment")
	}))

	req := httptest.NewRequest(http.MethodGet, "/flows", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	fields := decodeLog(t, buf.String())
	if fields["org_id"] != "org_42" {
		t.Fatalf("expected org_id propagated to downstream log, got %v", fields["org_id"])
	}
}

func TestNewSummaryIncludesMidRequestEnrichment(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := zerolog.New(&buf)

	// Inner handler stands in for auth middleware: it enriches the request
	// logger in place after Contextualize ran but before the summary line.
	enrich := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestlog.From(r.Context()).UpdateContext(func(c zerolog.Context) zerolog.Context {
			return c.Str("org_id", "org_99")
		})
	})
	handler := requestlog.Contextualize(base)(requestlog.New(base, requestlog.Options{})(enrich))

	req := httptest.NewRequest(http.MethodGet, "/flows", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	fields := decodeLog(t, buf.String())
	msg, _ := fields["message"].(string)
	if !strings.Contains(msg, "GET") || !strings.Contains(msg, "/flows") || !strings.Contains(msg, "200") {
		t.Fatalf("expected summary headline with verb/path/status, got %q", msg)
	}
	if fields["org_id"] != "org_99" {
		t.Fatalf("expected org_id on summary line, got %v", fields["org_id"])
	}
	if fields["request_id"] == nil || fields["request_id"] == "" {
		t.Fatalf("expected request_id on summary line, got %v", fields["request_id"])
	}
}

func TestFromWithoutContextualizeIsDisabled(t *testing.T) {
	t.Parallel()

	logger := requestlog.From(context.Background())
	if logger.GetLevel() != zerolog.Disabled {
		t.Fatalf("expected disabled logger without Contextualize, got level %v", logger.GetLevel())
	}
	if id := requestlog.RequestIDFromContext(context.Background()); id != "" {
		t.Fatalf("expected empty request id without Contextualize, got %q", id)
	}
}

func decodeLog(t *testing.T, raw string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(raw)
	if line == "" {
		t.Fatal("expected a log line, got none")
	}
	// Take the last emitted line in case multiple were written.
	if idx := strings.LastIndex(strings.TrimRight(line, "\n"), "\n"); idx >= 0 {
		line = line[idx+1:]
	}
	fields := map[string]any{}
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		t.Fatalf("failed to decode log line %q: %v", line, err)
	}
	return fields
}
