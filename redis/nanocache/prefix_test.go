package nanocache_test

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

// A prefix is used as given, so New refuses an empty one, one that a glob
// would read as a wildcard, or one that ends in the separator. A hierarchy of
// plain segments stays valid.
func TestNewRejectsAnEmptyPrefixOrAGlobCharacterOrATrailingSeparator(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	for _, prefix := range []string{"", "prod*", "prod?", "team[ops]", "team]", `a\b`, "billing:", ":"} {
		if _, err := nanocache.New[string](newFakeStore(), prefix, time.Minute, logger); !errors.Is(
			err, nanocache.ErrInvalidPrefix) {
			t.Errorf("New(%q) = %v, want ErrInvalidPrefix", prefix, err)
		}
	}
	for _, prefix := range []string{"product", "billing:product", "a:b:c", "api-key_v2"} {
		if _, err := nanocache.New[string](newFakeStore(), prefix, time.Minute, logger); err != nil {
			t.Errorf("New(%q) = %v, want a cache", prefix, err)
		}
	}
}
