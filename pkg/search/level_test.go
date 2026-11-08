package search_test

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// TestClientErrorsLogAtDebug pins that core/log logs a search error the caller
// caused at Debug, through any wrapping, and that errors.Is still matches.
func TestClientErrorsLogAtDebug(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{search.ErrInvalidCursor, search.ErrCursorWithOffset, search.ErrUnknownSort} {
		wrapped := fmt.Errorf("list flows: %w", fmt.Errorf("%w: not base64url", sentinel))
		if got := log.LevelFor(wrapped); got != slog.LevelDebug {
			t.Errorf("LevelFor(%v) = %v, want Debug", wrapped, got)
		}
		if !errors.Is(wrapped, sentinel) || !search.IsClientError(wrapped) {
			t.Errorf("%v no longer matches %v", wrapped, sentinel)
		}
	}
	if errors.Is(search.ErrInvalidCursor, search.ErrUnknownSort) ||
		errors.Is(search.ErrUnknownSort, search.ErrCursorWithOffset) {
		t.Error("the sentinels must stay distinct")
	}
	if _, err := search.DecodeCursor("!"); log.LevelFor(err) != slog.LevelDebug {
		t.Errorf("LevelFor(DecodeCursor error %v) = %v, want Debug", err, log.LevelFor(err))
	}
}
