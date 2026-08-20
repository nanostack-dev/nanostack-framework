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
	})

	t.Run("None is absent", func(t *testing.T) {
		o := functional.None[string]()
		if o.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if got := o.Value(); got != "" {
			t.Fatalf("Value() = %q, want the zero value", got)
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
		got := functional.None[string]().FlatMap(func(_ string) functional.Option[int] {
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

	t.Run("second lookup's absence propagates", func(t *testing.T) {
		got := functional.Some("missing").FlatMap(lookupB)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
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
}

func TestOptionOr(t *testing.T) {
	t.Run("keeps the value and skips f when present", func(t *testing.T) {
		called := false
		got := functional.Some(5).Or(func() functional.Option[int] {
			called = true
			return functional.Some(9)
		})
		if called {
			t.Fatalf("f was called on a present Option")
		}
		if !got.IsPresent() || got.Value() != 5 {
			t.Fatalf("Or(...) = %+v, want present 5", got)
		}
	})

	t.Run("substitutes the alternative when absent", func(t *testing.T) {
		got := functional.None[int]().Or(func() functional.Option[int] {
			return functional.Some(9)
		})
		if !got.IsPresent() || got.Value() != 9 {
			t.Fatalf("Or(...) = %+v, want present 9", got)
		}
	})

	t.Run("the alternative may itself be absent", func(t *testing.T) {
		got := functional.None[int]().Or(functional.None[int])
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})
}

func TestOptionPeek(t *testing.T) {
	t.Run("runs the side effect and returns the receiver when present", func(t *testing.T) {
		var seen []int
		got := functional.Some(5).Peek(func(n int) { seen = append(seen, n) })
		if len(seen) != 1 || seen[0] != 5 {
			t.Fatalf("Peek saw %v, want [5]", seen)
		}
		if !got.IsPresent() || got.Value() != 5 {
			t.Fatalf("Peek(...) = %+v, want present 5", got)
		}
	})

	t.Run("skips the side effect when absent", func(t *testing.T) {
		called := false
		got := functional.None[int]().Peek(func(int) { called = true })
		if called {
			t.Fatalf("f was called on an absent Option")
		}
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})
}

func TestOptionForEach(t *testing.T) {
	t.Run("runs f with the value when present", func(t *testing.T) {
		var seen []string
		functional.Some("value").ForEach(func(s string) { seen = append(seen, s) })
		if len(seen) != 1 || seen[0] != "value" {
			t.Fatalf("ForEach saw %v, want [value]", seen)
		}
	})

	t.Run("skips f when absent", func(t *testing.T) {
		called := false
		functional.None[string]().ForEach(func(string) { called = true })
		if called {
			t.Fatalf("f was called on an absent Option")
		}
	})
}

func TestOptionFold(t *testing.T) {
	onEmpty := func() string { return "empty" }
	label := func(n int) string { return fmt.Sprintf("n=%d", n) }

	t.Run("folds a present value through onPresent", func(t *testing.T) {
		if got := functional.Some(7).Fold(onEmpty, label); got != "n=7" {
			t.Fatalf("Fold(...) = %q, want %q", got, "n=7")
		}
	})

	t.Run("folds absence through onEmpty", func(t *testing.T) {
		if got := functional.None[int]().Fold(onEmpty, label); got != "empty" {
			t.Fatalf("Fold(...) = %q, want %q", got, "empty")
		}
	})
}

func TestOptionExists(t *testing.T) {
	isEven := func(n int) bool { return n%2 == 0 }

	t.Run("true for a present value that passes", func(t *testing.T) {
		if !functional.Some(4).Exists(isEven) {
			t.Fatalf("Exists(isEven) = false, want true")
		}
	})

	t.Run("false for a present value that fails", func(t *testing.T) {
		if functional.Some(3).Exists(isEven) {
			t.Fatalf("Exists(isEven) = true, want false")
		}
	})

	t.Run("false when absent, without calling pred", func(t *testing.T) {
		called := false
		if functional.None[int]().Exists(func(int) bool { called = true; return true }) {
			t.Fatalf("Exists(...) = true, want false")
		}
		if called {
			t.Fatalf("pred was called on an absent Option")
		}
	})
}

func TestOptionContains(t *testing.T) {
	t.Run("true for a present equal value", func(t *testing.T) {
		if !functional.Contains(functional.Some(5), 5) {
			t.Fatalf("Contains(Some(5), 5) = false, want true")
		}
	})

	t.Run("false for a present different value", func(t *testing.T) {
		if functional.Contains(functional.Some(5), 6) {
			t.Fatalf("Contains(Some(5), 6) = true, want false")
		}
	})

	t.Run("false when absent, even against the zero value", func(t *testing.T) {
		if functional.Contains(functional.None[int](), 0) {
			t.Fatalf("Contains(None, 0) = true, want false")
		}
	})
}

func TestOptionFromPtr(t *testing.T) {
	t.Run("a non-nil pointer becomes present", func(t *testing.T) {
		v := 5
		got := functional.FromPtr(&v)
		if !got.IsPresent() || got.Value() != 5 {
			t.Fatalf("FromPtr(&5) = %+v, want present 5", got)
		}
	})

	t.Run("a nil pointer becomes absence", func(t *testing.T) {
		got := functional.FromPtr[int](nil)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})

	t.Run("copies the value out instead of aliasing the pointer", func(t *testing.T) {
		v := 5
		got := functional.FromPtr(&v)
		v = 9
		if got.Value() != 5 {
			t.Fatalf("Value() = %d, want 5 — mutating *p reached into the Option", got.Value())
		}
	})
}

func TestOptionToPtr(t *testing.T) {
	t.Run("present becomes a pointer to a copy", func(t *testing.T) {
		o := functional.Some(5)
		p := o.ToPtr()
		if p == nil || *p != 5 {
			t.Fatalf("ToPtr() = %v, want a pointer to 5", p)
		}
		*p = 9
		if o.Value() != 5 {
			t.Fatalf("Value() = %d, want 5 — the pointer aliased the Option's interior", o.Value())
		}
	})

	t.Run("absent becomes nil", func(t *testing.T) {
		if p := functional.None[int]().ToPtr(); p != nil {
			t.Fatalf("ToPtr() = %v, want nil", p)
		}
	})
}

func TestOptionOf(t *testing.T) {
	t.Run("ok lifts the value into presence", func(t *testing.T) {
		got := functional.OptionOf(5, true)
		if !got.IsPresent() || got.Value() != 5 {
			t.Fatalf("OptionOf(5, true) = %+v, want present 5", got)
		}
	})

	t.Run("not-ok is absence and drops the value", func(t *testing.T) {
		got := functional.OptionOf(5, false)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if got.Value() != 0 {
			t.Fatalf("Value() = %d, want the zero value", got.Value())
		}
	})

	t.Run("lifts a map lookup directly", func(t *testing.T) {
		cache := map[string]int{"hit": 42}

		v, ok := cache["hit"]
		if got := functional.OptionOf(v, ok); !got.IsPresent() || got.Value() != 42 {
			t.Fatalf("OptionOf(cache[hit]) = %+v, want present 42", got)
		}

		v, ok = cache["miss"]
		if got := functional.OptionOf(v, ok); got.IsPresent() {
			t.Fatalf("OptionOf(cache[miss]) = %+v, want absent", got)
		}
	})
}

func TestOptionIsAbsent(t *testing.T) {
	t.Run("absent reports true and mirrors IsPresent", func(t *testing.T) {
		o := functional.None[string]()
		if !o.IsAbsent() {
			t.Fatalf("IsAbsent() = false, want true")
		}
		if o.IsAbsent() == o.IsPresent() {
			t.Fatalf("IsAbsent() and IsPresent() both = %t, want opposites", o.IsAbsent())
		}
	})

	t.Run("present reports false and mirrors IsPresent", func(t *testing.T) {
		o := functional.Some("value")
		if o.IsAbsent() {
			t.Fatalf("IsAbsent() = true, want false")
		}
		if o.IsAbsent() == o.IsPresent() {
			t.Fatalf("IsAbsent() and IsPresent() both = %t, want opposites", o.IsAbsent())
		}
	})

	t.Run("the zero Option is absent", func(t *testing.T) {
		var o functional.Option[int]
		if !o.IsAbsent() {
			t.Fatalf("IsAbsent() = false on the zero value, want true")
		}
	})

	t.Run("a filtered-out value becomes absent", func(t *testing.T) {
		o := functional.Some(3).Filter(func(n int) bool { return n > 10 })
		if !o.IsAbsent() {
			t.Fatalf("IsAbsent() = false after Filter rejected the value, want true")
		}
	})
}
