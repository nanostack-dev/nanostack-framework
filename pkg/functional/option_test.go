package functional_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestOption(t *testing.T) {
	t.Run("Some is present", func(t *testing.T) {
		o := functional.Some("value")
		if !o.IsPresent() {
			t.Fatalf("IsPresent() = false, want true")
		}
		if got := o.Value(); got != "value" {
			t.Fatalf("Value() = %q, want %q", got, "value")
		}
		if err := o.Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
	})

	t.Run("None is absent with no error", func(t *testing.T) {
		o := functional.None[string]()
		if o.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if err := o.Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
		if got := o.Value(); got != "" {
			t.Fatalf("Value() = %q, want the zero value", got)
		}
	})

	t.Run("Failed is not present and carries the error", func(t *testing.T) {
		sentinel := errors.New("boom")
		o := functional.Failed[string](sentinel)
		if o.IsPresent() {
			t.Fatalf("IsPresent() = true, want false — Err is non-nil")
		}
		if !errors.Is(o.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", o.Err(), sentinel)
		}
	})
}

func TestOptionMap(t *testing.T) {
	double := func(n int) int { return n * 2 }

	t.Run("maps the value when present", func(t *testing.T) {
		got := functional.Some(21).Map(double)
		if !got.IsPresent() || got.Value() != 42 {
			t.Fatalf("Map(double) = %+v, want present 42", got)
		}
	})

	t.Run("leaves absence untouched", func(t *testing.T) {
		got := functional.None[int]().Map(double)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false — mapping absence stays absent")
		}
	})

	t.Run("leaves a failure untouched", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Failed[int](sentinel).Map(double)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("changes type, not just value", func(t *testing.T) {
		toLabel := func(n int) string { return fmt.Sprintf("n=%d", n) }
		got := functional.Some(7).Map(toLabel)
		if got.Value() != "n=7" {
			t.Fatalf("Value() = %q, want %q", got.Value(), "n=7")
		}
	})
}

func TestOptionFlatMap(t *testing.T) {
	lookupB := func(a string) functional.Option[int] {
		if a == "found" {
			return functional.Some(42)
		}
		return functional.None[int]()
	}

	t.Run("chains into a second present lookup", func(t *testing.T) {
		got := functional.Some("found").FlatMap(lookupB)
		if !got.IsPresent() || got.Value() != 42 {
			t.Fatalf("FlatMap = %+v, want present 42", got)
		}
	})

	t.Run("first absence short-circuits before f runs", func(t *testing.T) {
		called := false
		got := functional.None[string]().FlatMap(func(a string) functional.Option[int] {
			called = true
			return functional.Some(1)
		})
		if called {
			t.Fatalf("f was called on an absent receiver")
		}
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})

	t.Run("first failure short-circuits and propagates the error", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Failed[string](sentinel).FlatMap(lookupB)
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("second lookup's absence propagates", func(t *testing.T) {
		got := functional.Some("missing").FlatMap(lookupB)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if err := got.Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
	})

	t.Run("second lookup's failure propagates", func(t *testing.T) {
		sentinel := errors.New("second boom")
		got := functional.Some("found").FlatMap(func(a string) functional.Option[int] {
			return functional.Failed[int](sentinel)
		})
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})
}

func TestOptionFilter(t *testing.T) {
	isEven := func(n int) bool { return n%2 == 0 }

	t.Run("keeps a value that passes", func(t *testing.T) {
		got := functional.Some(4).Filter(isEven)
		if !got.IsPresent() || got.Value() != 4 {
			t.Fatalf("Filter(isEven) = %+v, want present 4", got)
		}
	})

	t.Run("drops a value that fails", func(t *testing.T) {
		got := functional.Some(3).Filter(isEven)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})

	t.Run("leaves absence untouched", func(t *testing.T) {
		got := functional.None[int]().Filter(isEven)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})

	t.Run("leaves a failure untouched", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Failed[int](sentinel).Filter(isEven)
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})
}

func TestOptionOrElse(t *testing.T) {
	t.Run("returns the value when present", func(t *testing.T) {
		if got := functional.Some(5).OrElse(0); got != 5 {
			t.Fatalf("OrElse(0) = %d, want 5", got)
		}
	})

	t.Run("returns the fallback when absent", func(t *testing.T) {
		if got := functional.None[int]().OrElse(9); got != 9 {
			t.Fatalf("OrElse(9) = %d, want 9", got)
		}
	})

	t.Run("returns the fallback on failure", func(t *testing.T) {
		if got := functional.Failed[int](errors.New("boom")).OrElse(9); got != 9 {
			t.Fatalf("OrElse(9) = %d, want 9", got)
		}
	})
}

func TestOptionOrElseGet(t *testing.T) {
	t.Run("does not call the fallback when present", func(t *testing.T) {
		called := false
		got := functional.Some(5).OrElseGet(func() int {
			called = true
			return 9
		})
		if called {
			t.Fatalf("fallback was called on a present Option")
		}
		if got != 5 {
			t.Fatalf("OrElseGet(...) = %d, want 5", got)
		}
	})

	t.Run("calls the fallback when absent", func(t *testing.T) {
		if got := functional.None[int]().OrElseGet(func() int { return 9 }); got != 9 {
			t.Fatalf("OrElseGet(...) = %d, want 9", got)
		}
	})
}

func TestOptionGet(t *testing.T) {
	t.Run("reports ok when present", func(t *testing.T) {
		v, ok := functional.Some("value").Get()
		if !ok || v != "value" {
			t.Fatalf("Get() = (%q, %t), want (\"value\", true)", v, ok)
		}
	})

	t.Run("reports not-ok when absent", func(t *testing.T) {
		v, ok := functional.None[string]().Get()
		if ok || v != "" {
			t.Fatalf("Get() = (%q, %t), want (\"\", false)", v, ok)
		}
	})

	t.Run("reports not-ok on failure", func(t *testing.T) {
		if _, ok := functional.Failed[string](errors.New("boom")).Get(); ok {
			t.Fatalf("Get() ok = true, want false")
		}
	})
}

func TestOptionToResult(t *testing.T) {
	errAbsent := errors.New("not found")

	t.Run("a present value becomes a successful Result", func(t *testing.T) {
		v, err := functional.Some(5).ToResult(errAbsent).Value()
		if err != nil || v != 5 {
			t.Fatalf("ToResult(...).Value() = (%d, %v), want (5, nil)", v, err)
		}
	})

	t.Run("absence becomes the supplied error", func(t *testing.T) {
		err := functional.None[int]().ToResult(errAbsent).Err()
		if !errors.Is(err, errAbsent) {
			t.Fatalf("Err() = %v, want %v", err, errAbsent)
		}
	})

	t.Run("an existing failure outranks the absent error", func(t *testing.T) {
		// The lookup genuinely failed; reporting that as the caller's
		// not-found sentinel would turn an outage into a 404.
		sentinel := errors.New("connection refused")
		err := functional.Failed[int](sentinel).ToResult(errAbsent).Err()
		if !errors.Is(err, sentinel) {
			t.Fatalf("Err() = %v, want the original failure %v", err, sentinel)
		}
	})
}
