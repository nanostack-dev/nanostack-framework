package functional_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestResult(t *testing.T) {
	t.Run("Ok carries the value and a nil error", func(t *testing.T) {
		r := functional.Ok("value")
		if !r.IsOk() {
			t.Fatalf("IsOk() = false, want true")
		}
		v, err := r.Value()
		if v != "value" || err != nil {
			t.Fatalf("Value() = (%q, %v), want (\"value\", nil)", v, err)
		}
	})

	t.Run("Failure carries the error", func(t *testing.T) {
		sentinel := errors.New("boom")
		r := functional.Failure[string](sentinel)
		if r.IsOk() {
			t.Fatalf("IsOk() = true, want false")
		}
		if !errors.Is(r.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", r.Err(), sentinel)
		}
	})
}

func TestResultMap(t *testing.T) {
	double := func(n int) int { return n * 2 }

	t.Run("maps the value on success", func(t *testing.T) {
		got := functional.Ok(21).Map(double)
		v, err := got.Value()
		if err != nil || v != 42 {
			t.Fatalf("Map(double).Value() = (%d, %v), want (42, nil)", v, err)
		}
	})

	t.Run("leaves a failure untouched", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Failure[int](sentinel).Map(double)
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("changes type, not just value", func(t *testing.T) {
		toLabel := func(n int) string { return fmt.Sprintf("n=%d", n) }
		got := functional.Ok(7).Map(toLabel)
		v, _ := got.Value()
		if v != "n=7" {
			t.Fatalf("Value() = %q, want %q", v, "n=7")
		}
	})
}

func TestResultFlatMap(t *testing.T) {
	step2 := func(n int) functional.Result[string] {
		return functional.Ok(fmt.Sprintf("n=%d", n))
	}

	t.Run("chains into a second successful step", func(t *testing.T) {
		got := functional.Ok(42).FlatMap(step2)
		v, err := got.Value()
		if err != nil || v != "n=42" {
			t.Fatalf("FlatMap(step2).Value() = (%q, %v), want (\"n=42\", nil)", v, err)
		}
	})

	t.Run("first failure short-circuits before f runs", func(t *testing.T) {
		sentinel := errors.New("boom")
		called := false
		got := functional.Failure[int](sentinel).FlatMap(func(n int) functional.Result[string] {
			called = true
			return functional.Ok("unused")
		})
		if called {
			t.Fatalf("f was called on a failed receiver")
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("second step's failure propagates", func(t *testing.T) {
		sentinel := errors.New("second boom")
		got := functional.Ok(1).FlatMap(func(n int) functional.Result[string] {
			return functional.Failure[string](sentinel)
		})
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})
}

func TestResultMapErr(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("wrapped: %w", err) }

	t.Run("transforms the error on failure", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Failure[int](sentinel).MapErr(wrap)
		if got.Err() == nil || !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want it to wrap %v", got.Err(), sentinel)
		}
	})

	t.Run("leaves success untouched", func(t *testing.T) {
		got := functional.Ok(5).MapErr(wrap)
		v, err := got.Value()
		if err != nil || v != 5 {
			t.Fatalf("Value() = (%d, %v), want (5, nil)", v, err)
		}
	})

	t.Run("preserves the value that was already there when translating", func(t *testing.T) {
		// Mirrors how transactor.Result.OnUnique translates a driver error
		// while keeping T's existing value — MapErr must not zero it out.
		sentinel := errors.New("boom")
		got := functional.Ok(5).MapErr(func(error) error { return sentinel }) // no-op: Ok has no error to translate
		if v, err := got.Value(); err != nil || v != 5 {
			t.Fatalf("Value() = (%d, %v), want (5, nil) — MapErr is a no-op on success", v, err)
		}
	})
}
