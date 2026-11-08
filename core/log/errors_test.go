package log_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
)

// problemError carries an HTTP status the way core/fault's Described errors
// do, through ProblemStatus.
type problemError struct {
	status int
	cause  error
}

func (e problemError) Error() string      { return http.StatusText(e.status) }
func (e problemError) ProblemStatus() int { return e.status }
func (e problemError) Unwrap() error      { return e.cause }

func notFound() error   { return problemError{status: http.StatusNotFound} }
func badRequest() error { return problemError{status: http.StatusBadRequest} }
func internal(cause error) error {
	return problemError{status: http.StatusInternalServerError, cause: cause}
}

// describedError implements core/fault's Described.
type describedError struct {
	status int
	typ    string
}

func (e describedError) Error() string        { return http.StatusText(e.status) }
func (e describedError) ProblemStatus() int   { return e.status }
func (e describedError) ProblemType() string  { return e.typ }
func (e describedError) ProblemTitle() string { return http.StatusText(e.status) }

type clientMistake struct{}

func (clientMistake) Error() string     { return "invalid cursor" }
func (clientMistake) Level() slog.Level { return slog.LevelDebug }

// driverError has the SQLSTATE method of *pq.Error and *pgconn.PgError.
type driverError struct{ code, message string }

func (e *driverError) Error() string    { return "pq: " + e.message + " (" + e.code + ")" }
func (e *driverError) SQLState() string { return e.code }

var (
	cancelledStatement = &driverError{code: "57014", message: "canceling statement due to user request"}
	timedOutStatement  = &driverError{code: "57014", message: "canceling statement due to statement timeout"}
)

func TestIsContextError(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]bool{
		context.Canceled:                                 true,
		context.DeadlineExceeded:                         true,
		fmt.Errorf("list flows: %w", cancelledStatement): true,
		timedOutStatement:                                false,
		&driverError{code: "23505", message: "duplicate key due to user request"}:      false,
		errors.New("pq: canceling statement due to user request (57014)"):              false,
		internal(fmt.Errorf("jet: %w", cancelledStatement)):                            true,
		errors.Join(errors.New("rollback"), &driverError{code: "57014", message: "x"}): false,
	} {
		if got := log.IsContextError(err); got != want {
			t.Errorf("IsContextError(%v) = %v, want %v", err, got, want)
		}
	}
}

func TestLevelFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want slog.Level
	}{
		{"nil", nil, slog.LevelError},
		{"plain", errors.New("boom"), slog.LevelError},
		{"canceled", fmt.Errorf("query: %w", context.Canceled), slog.LevelWarn},
		{"deadline", context.DeadlineExceeded, slog.LevelWarn},
		{"not found", notFound(), slog.LevelDebug},
		{"wrapped bad request", fmt.Errorf("handler: %w", badRequest()), slog.LevelDebug},
		{"internal fault", internal(nil), slog.LevelError},
		{"canceled inside a fault", internal(context.Canceled), slog.LevelWarn},
		{"statement cancelled by the client", fmt.Errorf("query: %w", cancelledStatement), slog.LevelWarn},
		{"statement timeout", fmt.Errorf("query: %w", timedOutStatement), slog.LevelError},
		{"self-levelled", fmt.Errorf("search: %w", clientMistake{}), slog.LevelDebug},
		// The problem writer renders a status outside 400 to 599 as a 500.
		{"status 200 is not an error status", problemError{status: http.StatusOK}, slog.LevelError},
		{"status 0 is not an error status", problemError{}, slog.LevelError},
		// A Described with an empty type renders as a 500 (fault.Valid).
		{
			"described 404",
			describedError{status: http.StatusNotFound, typ: "https://errors.example/x"},
			slog.LevelDebug,
		},
		{"described 404 without a type", describedError{status: http.StatusNotFound}, slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := log.LevelFor(tt.err); got != tt.want {
				t.Fatalf("LevelFor = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorToAttachesErrorStatusAndCallerSource(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	err := notFound()

	log.ErrorTo(t.Context(), logger, err, "load flow", "flow_id", "f1")

	entry := rec.AssertLogged(t, slog.LevelDebug, "load flow",
		log.Err(err), slog.Int("status", http.StatusNotFound), slog.String("flow_id", "f1"))
	if src := entry.Source(); src == nil || !strings.HasSuffix(src.File, "errors_test.go") {
		t.Fatalf("source = %+v, want this file", src)
	}
}

func TestErrorUsesDefaultLoggerWithContext(t *testing.T) {
	t.Parallel()
	ctx, rec := logtest.Capture(t)
	err := errors.New("boom")

	log.Error(ctx, err, "save flow")

	entry := rec.AssertLogged(t, slog.LevelError, "save flow", log.Err(err))
	if _, ok := entry.Value("status"); ok {
		t.Fatal("plain error got a status")
	}
	if src := entry.Source(); src == nil || !strings.HasSuffix(src.File, "errors_test.go") {
		t.Fatalf("source = %+v, want this file", src)
	}
}

// TestErrorStatusToUsesTheChosenStatus covers a caller that already chose the
// response status for an error that carries none: the level and the status
// attribute follow that status, and the rules of LevelFor still apply.
func TestErrorStatusToUsesTheChosenStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		want   slog.Level
	}{
		{
			"body too large",
			errors.New("http: request body too large"),
			http.StatusRequestEntityTooLarge,
			slog.LevelDebug,
		},
		{"unknown error", errors.New("db down"), http.StatusInternalServerError, slog.LevelError},
		{"client closed", context.Canceled, 499, slog.LevelWarn},
		{"self-levelled", clientMistake{}, http.StatusInternalServerError, slog.LevelDebug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, rec := logtest.New()
			log.ErrorStatusTo(t.Context(), logger, tt.err, tt.status, "request failed")
			entry := rec.AssertLogged(t, tt.want, "request failed", log.Err(tt.err), slog.Int(log.KeyStatus, tt.status))
			if src := entry.Source(); src == nil || !strings.HasSuffix(src.File, "errors_test.go") {
				t.Fatalf("source = %+v, want this file", src)
			}
		})
	}
}

func TestErrorToSkipsDisabledLevel(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out}) // info

	log.ErrorTo(t.Context(), logger, notFound(), "not found is debug")

	if out.String() != "" {
		t.Fatalf("got %s", out.String())
	}
}
