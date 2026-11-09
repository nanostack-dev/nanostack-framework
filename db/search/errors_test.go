package search_test

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// leveledError is the shape core/log's LevelFor looks for in a chain.
type leveledError interface {
	error
	Level() slog.Level
}

// clientErrors are the search errors that answer HTTP 400.
var clientErrors = []error{ //nolint:gochecknoglobals // read-only test table.
	search.ErrInvalidCursor,
	search.ErrCursorWithOffset,
	search.ErrUnknownSort,
	search.ErrInvalidLimit,
	search.ErrInvalidOffset,
	search.ErrTooManyValues,
	search.ErrTermTooLong,
}

func TestIsClientError(t *testing.T) {
	t.Parallel()
	for _, sentinel := range clientErrors {
		wrapped := fmt.Errorf("search flows: %w", fmt.Errorf("%w: detail", sentinel))
		if !search.IsClientError(sentinel) || !search.IsClientError(wrapped) {
			t.Errorf("IsClientError(%v) = false, want true", wrapped)
		}
	}
	for name, err := range map[string]error{
		"nil":                                   nil,
		"unscoped":                              search.ErrUnscoped,
		"wrapped unscoped":                      fmt.Errorf("search flows: %w", search.ErrUnscoped),
		"unscoped joined after a client error":  errors.Join(search.ErrTooManyValues, search.ErrUnscoped),
		"unscoped joined before a client error": errors.Join(search.ErrUnscoped, search.ErrTermTooLong),
		"plain error":                           errors.New("connection refused"),
	} {
		if search.IsClientError(err) {
			t.Errorf("IsClientError(%s: %v) = true, want false", name, err)
		}
	}
}

// TestClientErrorsCarryDebugLevel pins the slog.Leveler that core/log's
// LevelFor reads: the API refusing a bad request is not the service failing.
func TestClientErrorsCarryDebugLevel(t *testing.T) {
	t.Parallel()
	for _, sentinel := range clientErrors {
		wrapped := fmt.Errorf("list flows: %w", sentinel)
		leveler, ok := errors.AsType[leveledError](wrapped)
		if !ok {
			t.Errorf("%v has no Level method", sentinel)
			continue
		}
		if got := leveler.Level(); got != slog.LevelDebug {
			t.Errorf("%v Level() = %v, want Debug", sentinel, got)
		}
	}
}

// TestUnscopedIsAProgrammingError pins that ErrUnscoped picks no level of its
// own, so core/log logs it at Error, and the HTTP edge answers it with a 500.
func TestUnscopedIsAProgrammingError(t *testing.T) {
	t.Parallel()
	if _, ok := errors.AsType[leveledError](search.ErrUnscoped); ok {
		t.Fatal("ErrUnscoped has a Level method; it must log at Error")
	}
}

func TestSentinelsAreDistinct(t *testing.T) {
	t.Parallel()
	all := append([]error{search.ErrUnscoped}, clientErrors...)
	for i, a := range all {
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = true; the sentinels must stay distinct", a, b)
			}
		}
	}
}

func TestSentinelTexts(t *testing.T) {
	t.Parallel()
	// The first three texts are those of pkg/search: logs and alerts match them.
	for err, want := range map[error]string{
		search.ErrInvalidCursor:    "search: invalid cursor",
		search.ErrCursorWithOffset: "search: cursor cannot be combined with offset",
		search.ErrUnknownSort:      "search: unknown sort field",
	} {
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	}
}
