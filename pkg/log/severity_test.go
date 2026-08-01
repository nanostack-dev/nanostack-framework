package log_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
)

func TestLevelFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want zerolog.Level
	}{
		{
			name: "nil error defaults to error",
			err:  nil,
			want: zerolog.ErrorLevel,
		},
		{
			// anchor's rule: a client that hung up is not a server fault.
			name: "context cancelled is warn",
			err:  context.Canceled,
			want: zerolog.WarnLevel,
		},
		{
			name: "deadline exceeded is warn",
			err:  context.DeadlineExceeded,
			want: zerolog.WarnLevel,
		},
		{
			name: "wrapped cancellation is warn",
			err:  fmt.Errorf("load flow: %w", context.Canceled),
			want: zerolog.WarnLevel,
		},
		{
			// echopoint's rule: a handled 4xx is the API answering correctly.
			name: "not found is debug",
			err:  fault.NotFound("FLOW_NOT_FOUND", "flow not found"),
			want: zerolog.DebugLevel,
		},
		{
			name: "bad request is debug",
			err:  fault.BadRequest("INVALID", "invalid payload"),
			want: zerolog.DebugLevel,
		},
		{
			name: "wrapped not found is debug",
			err:  fmt.Errorf("get flow: %w", fault.NotFound("FLOW_NOT_FOUND", "flow not found")),
			want: zerolog.DebugLevel,
		},
		{
			name: "internal fault stays error",
			err:  fault.Internal("BOOM", "boom"),
			want: zerolog.ErrorLevel,
		},
		{
			name: "plain error stays error",
			err:  errors.New("connection refused"),
			want: zerolog.ErrorLevel,
		},
		{
			// A cancelled request often also carries a fault from the layer
			// that gave up. The cancellation is the truer story, so it wins.
			name: "cancellation beats a wrapping fault",
			err:  fault.Internal("UPSTREAM", "upstream failed").Wrap(context.Canceled),
			want: zerolog.WarnLevel,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := log.LevelFor(test.err); got != test.want {
				t.Fatalf("LevelFor(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

func TestEventAttachesErrorAndStatus(t *testing.T) {
	var sink capture
	logger := zerolog.New(&sink).Level(zerolog.DebugLevel)

	log.Event(&logger, fault.NotFound("FLOW_NOT_FOUND", "flow not found")).Msg("failed to get flow")

	line := sink.decode(t)
	if line["level"] != "debug" {
		t.Fatalf("level = %v, want debug", line["level"])
	}
	if line["status"] != float64(http.StatusNotFound) {
		t.Fatalf("status = %v, want 404", line["status"])
	}
	if line["error"] == nil {
		t.Fatal("expected the error to be attached without the caller adding it")
	}
	if line["message"] != "failed to get flow" {
		t.Fatalf("message = %v", line["message"])
	}
}

func TestEventOmitsStatusForPlainError(t *testing.T) {
	var sink capture
	logger := zerolog.New(&sink).Level(zerolog.DebugLevel)

	log.Event(&logger, errors.New("connection refused")).Msg("query failed")

	line := sink.decode(t)
	if line["level"] != "error" {
		t.Fatalf("level = %v, want error", line["level"])
	}
	if _, ok := line["status"]; ok {
		t.Fatal("a non-API error should not carry an HTTP status")
	}
}

func TestEventNilLoggerDoesNotPanic(_ *testing.T) {
	log.Event(nil, errors.New("boom")).Msg("should be discarded")
}

func TestErrorUsesContextLogger(t *testing.T) {
	var sink capture
	logger := zerolog.New(&sink).Level(zerolog.DebugLevel).With().Str("org_id", "org_123").Logger()
	ctx := logger.WithContext(context.Background())

	log.Error(ctx, fault.NotFound("GONE", "gone")).Msg("lookup failed")

	line := sink.decode(t)
	if line["org_id"] != "org_123" {
		t.Fatalf("expected the context logger's fields, got %v", line)
	}
	if line["level"] != "debug" {
		t.Fatalf("level = %v, want debug", line["level"])
	}
}

// capture collects the most recent log line written to it.
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
