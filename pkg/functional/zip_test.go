package functional_test

import (
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// The Zip family is generated from one template per shape, so these tests
// cover the semantics thoroughly at arity 2 and then spot-check that a higher
// arity wires its inputs through in the right order.

func TestZipOption2(t *testing.T) {
	t.Run("both present yields a present tuple", func(t *testing.T) {
		got := functional.ZipOption2(functional.Some("a"), functional.Some(1))
		if !got.IsPresent() {
			t.Fatalf("IsPresent() = false, want true")
		}
		first, second := got.Value().Unpack()
		if first != "a" || second != 1 {
			t.Fatalf("Value().Unpack() = (%q, %d), want (\"a\", 1)", first, second)
		}
	})

	t.Run("either absent yields absence", func(t *testing.T) {
		tests := map[string]functional.Option[functional.Tuple2[string, int]]{
			"first absent":  functional.ZipOption2(functional.None[string](), functional.Some(1)),
			"second absent": functional.ZipOption2(functional.Some("a"), functional.None[int]()),
			"both absent":   functional.ZipOption2(functional.None[string](), functional.None[int]()),
		}
		for name, got := range tests {
			t.Run(name, func(t *testing.T) {
				if got.IsPresent() {
					t.Fatalf("IsPresent() = true, want false")
				}
			})
		}
	})
}

// TestZipOption4 spot-checks a higher arity: the inputs must land in the tuple
// in the order they were passed, which is where a template indexing slip shows.
func TestZipOption4(t *testing.T) {
	got := functional.ZipOption4(
		functional.Some(1),
		functional.Some("two"),
		functional.Some(3.0),
		functional.Some(true),
	)
	if !got.IsPresent() {
		t.Fatalf("IsPresent() = false, want true")
	}

	a, b, c, d := got.Value().Unpack()
	if a != 1 || b != "two" || c != 3.0 || !d {
		t.Fatalf("Unpack() = (%d, %q, %v, %t), want (1, \"two\", 3, true)", a, b, c, d)
	}

	t.Run("one absent input makes the whole zip absent", func(t *testing.T) {
		absent := functional.ZipOption4(
			functional.Some(1),
			functional.Some("two"),
			functional.None[float64](),
			functional.Some(true),
		)
		if absent.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})
}

func TestZipResult2(t *testing.T) {
	errFirst := errors.New("first boom")
	errSecond := errors.New("second boom")

	t.Run("both ok yields a tuple", func(t *testing.T) {
		got := functional.ZipResult2(functional.Ok("a"), functional.Ok(1))
		pair, err := got.Value()
		if err != nil {
			t.Fatalf("Value() err = %v, want nil", err)
		}
		first, second := pair.Unpack()
		if first != "a" || second != 1 {
			t.Fatalf("Unpack() = (%q, %d), want (\"a\", 1)", first, second)
		}
	})

	t.Run("the leftmost error wins", func(t *testing.T) {
		got := functional.ZipResult2(
			functional.Failure[string](errFirst),
			functional.Failure[int](errSecond),
		)
		if !errors.Is(got.Err(), errFirst) {
			t.Fatalf("Err() = %v, want the leftmost error %v", got.Err(), errFirst)
		}
	})

	t.Run("a later failure is still reported", func(t *testing.T) {
		got := functional.ZipResult2(functional.Ok("a"), functional.Failure[int](errSecond))
		if !errors.Is(got.Err(), errSecond) {
			t.Fatalf("Err() = %v, want %v", got.Err(), errSecond)
		}
	})
}
