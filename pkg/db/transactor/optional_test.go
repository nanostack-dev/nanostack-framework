package transactor_test

import (
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// Optional's own present/absent/failure/Map/FlatMap/Filter/OrElse behavior is
// exercised by pkg/functional's tests — Optional is a type alias for
// functional.Option[T], not a reimplementation. What's worth pinning here is
// the alias itself: a transactor.Optional[T] built via functional's
// constructors behaves exactly like a functional.Option[T], and repository
// code can chain two Optional-producing lookups with FlatMap the way the
// package doc shows.
func TestOptionalIsFunctionalOption(t *testing.T) {
	var _ transactor.Optional[string] = functional.Some("value")

	t.Run("round-trips through functional's constructors", func(t *testing.T) {
		o := functional.Some(21)
		if !o.IsPresent() || o.Value() != 21 {
			t.Fatalf("Optional built via functional.Some = %+v, want present 21", o)
		}
	})
}

// TestOptionalFlatMapChainsTwoLookups pins the pattern the package doc
// promises: look up A, then look up B keyed by A, with absence or failure
// from either step propagating through the chain the same way.
func TestOptionalFlatMapChainsTwoLookups(t *testing.T) {
	lookupA := func(found bool) transactor.Optional[string] {
		if found {
			return functional.Some("a")
		}
		return functional.None[string]()
	}
	lookupB := func(a string) transactor.Optional[int] {
		if a == "a" {
			return functional.Some(len(a))
		}
		return functional.None[int]()
	}

	t.Run("both present chains through", func(t *testing.T) {
		got := functional.FlatMapOption(lookupA(true), lookupB)
		if !got.IsPresent() || got.Value() != 1 {
			t.Fatalf("FlatMap chain = %+v, want present 1", got)
		}
	})

	t.Run("first absence short-circuits", func(t *testing.T) {
		got := functional.FlatMapOption(lookupA(false), lookupB)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})

	t.Run("failure propagates through the chain", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.FlatMapOption(functional.Failed[string](sentinel), lookupB)
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})
}
