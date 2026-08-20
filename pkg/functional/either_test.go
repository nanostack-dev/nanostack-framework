package functional_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestEither(t *testing.T) {
	t.Run("Left holds the left possibility", func(t *testing.T) {
		e := functional.Left[string, int]("cached")
		if !e.IsLeft() || e.IsRight() {
			t.Fatalf("IsLeft() = %t, IsRight() = %t, want (true, false)", e.IsLeft(), e.IsRight())
		}
		l, ok := e.Left()
		if !ok || l != "cached" {
			t.Fatalf("Left() = (%q, %t), want (\"cached\", true)", l, ok)
		}
		if r, rightOK := e.Right(); rightOK {
			t.Fatalf("Right() = (%d, true), want ok false", r)
		}
	})

	t.Run("Right holds the right possibility", func(t *testing.T) {
		e := functional.Right[string](42)
		if e.IsLeft() || !e.IsRight() {
			t.Fatalf("IsLeft() = %t, IsRight() = %t, want (false, true)", e.IsLeft(), e.IsRight())
		}
		r, ok := e.Right()
		if !ok || r != 42 {
			t.Fatalf("Right() = (%d, %t), want (42, true)", r, ok)
		}
		if l, leftOK := e.Left(); leftOK {
			t.Fatalf("Left() = (%q, true), want ok false", l)
		}
	})

	t.Run("zero value is a left of L's zero value", func(t *testing.T) {
		var e functional.Either[string, int]
		if !e.IsLeft() {
			t.Fatalf("IsLeft() = false, want true — the zero Either must be a left")
		}
		l, ok := e.Left()
		if !ok || l != "" {
			t.Fatalf("Left() = (%q, %t), want (\"\", true)", l, ok)
		}
		if got := e.Map(func(n int) int { return n * 2 }).GetOrElse(-1); got != -1 {
			t.Fatalf("Map on the zero value reached f, GetOrElse = %d, want -1", got)
		}
	})
}

func TestEitherMap(t *testing.T) {
	double := func(n int) int { return n * 2 }

	t.Run("maps the right value", func(t *testing.T) {
		got := functional.Right[string](21).Map(double)
		if r, ok := got.Right(); !ok || r != 42 {
			t.Fatalf("Map(double).Right() = (%d, %t), want (42, true)", r, ok)
		}
	})

	t.Run("leaves a left untouched and does not call f", func(t *testing.T) {
		called := false
		got := functional.Left[string, int]("skip").Map(func(n int) int {
			called = true
			return n
		})
		if called {
			t.Fatalf("f was called on a left receiver")
		}
		if l, ok := got.Left(); !ok || l != "skip" {
			t.Fatalf("Left() = (%q, %t), want (\"skip\", true)", l, ok)
		}
	})

	t.Run("changes the right type while the left type stays", func(t *testing.T) {
		toLabel := func(n int) string { return fmt.Sprintf("n=%d", n) }
		got := functional.Right[error](7).Map(toLabel)
		if r, ok := got.Right(); !ok || r != "n=7" {
			t.Fatalf("Right() = (%q, %t), want (\"n=7\", true)", r, ok)
		}
	})
}

func TestEitherFlatMap(t *testing.T) {
	lookup := func(key string) functional.Either[string, int] {
		if key == "found" {
			return functional.Right[string](42)
		}
		return functional.Left[string, int]("missing " + key)
	}

	t.Run("chains into a second right", func(t *testing.T) {
		got := functional.Right[string]("found").FlatMap(lookup)
		if r, ok := got.Right(); !ok || r != 42 {
			t.Fatalf("FlatMap.Right() = (%d, %t), want (42, true)", r, ok)
		}
	})

	t.Run("first left short-circuits before f runs", func(t *testing.T) {
		called := false
		got := functional.Left[string, string]("early").FlatMap(func(string) functional.Either[string, int] {
			called = true
			return functional.Right[string](1)
		})
		if called {
			t.Fatalf("f was called on a left receiver")
		}
		if l, ok := got.Left(); !ok || l != "early" {
			t.Fatalf("Left() = (%q, %t), want (\"early\", true)", l, ok)
		}
	})

	t.Run("second step's left propagates", func(t *testing.T) {
		got := functional.Right[string]("absent").FlatMap(lookup)
		if l, ok := got.Left(); !ok || l != "missing absent" {
			t.Fatalf("Left() = (%q, %t), want (\"missing absent\", true)", l, ok)
		}
	})

	t.Run("chains with a changed right type", func(t *testing.T) {
		got := functional.Right[string](21).
			Map(func(n int) int { return n * 2 }).
			FlatMap(func(n int) functional.Either[string, string] {
				return functional.Right[string](fmt.Sprintf("n=%d", n))
			})
		if r, ok := got.Right(); !ok || r != "n=42" {
			t.Fatalf("Right() = (%q, %t), want (\"n=42\", true)", r, ok)
		}
	})
}

func TestEitherMapLeft(t *testing.T) {
	t.Run("maps the left value to a new type", func(t *testing.T) {
		got := functional.Left[string, int]("nope").MapLeft(errors.New)
		l, ok := got.Left()
		if !ok || l.Error() != "nope" {
			t.Fatalf("Left() = (%v, %t), want an error \"nope\" and true", l, ok)
		}
	})

	t.Run("leaves a right untouched and does not call f", func(t *testing.T) {
		called := false
		got := functional.Right[string](7).MapLeft(func(string) error {
			called = true
			return nil
		})
		if called {
			t.Fatalf("f was called on a right receiver")
		}
		if r, ok := got.Right(); !ok || r != 7 {
			t.Fatalf("Right() = (%d, %t), want (7, true)", r, ok)
		}
	})
}

func TestEitherFold(t *testing.T) {
	onLeft := func(s string) string { return "left:" + s }
	onRight := func(n int) string { return fmt.Sprintf("right:%d", n) }

	t.Run("folds the left side", func(t *testing.T) {
		if got := functional.Left[string, int]("x").Fold(onLeft, onRight); got != "left:x" {
			t.Fatalf("Fold = %q, want %q", got, "left:x")
		}
	})

	t.Run("folds the right side", func(t *testing.T) {
		if got := functional.Right[string](3).Fold(onLeft, onRight); got != "right:3" {
			t.Fatalf("Fold = %q, want %q", got, "right:3")
		}
	})
}

func TestEitherSwap(t *testing.T) {
	t.Run("a left becomes a right", func(t *testing.T) {
		got := functional.Left[string, int]("x").Swap()
		if r, ok := got.Right(); !ok || r != "x" {
			t.Fatalf("Swap().Right() = (%q, %t), want (\"x\", true)", r, ok)
		}
	})

	t.Run("a right becomes a left", func(t *testing.T) {
		got := functional.Right[string](3).Swap()
		if l, ok := got.Left(); !ok || l != 3 {
			t.Fatalf("Swap().Left() = (%d, %t), want (3, true)", l, ok)
		}
	})

	t.Run("swapping twice restores the original", func(t *testing.T) {
		got := functional.Right[string](3).Swap().Swap()
		if r, ok := got.Right(); !ok || r != 3 {
			t.Fatalf("Swap().Swap().Right() = (%d, %t), want (3, true)", r, ok)
		}
	})
}

func TestEitherGetOrElse(t *testing.T) {
	t.Run("returns the right value", func(t *testing.T) {
		if got := functional.Right[string](5).GetOrElse(0); got != 5 {
			t.Fatalf("GetOrElse(0) = %d, want 5", got)
		}
	})

	t.Run("returns the fallback on a left", func(t *testing.T) {
		if got := functional.Left[string, int]("x").GetOrElse(9); got != 9 {
			t.Fatalf("GetOrElse(9) = %d, want 9", got)
		}
	})
}

func TestEitherOrElse(t *testing.T) {
	other := functional.Right[string](9)

	t.Run("keeps the receiver when it is a right", func(t *testing.T) {
		got := functional.Right[string](5).OrElse(other)
		if r, ok := got.Right(); !ok || r != 5 {
			t.Fatalf("OrElse.Right() = (%d, %t), want (5, true)", r, ok)
		}
	})

	t.Run("returns other on a left", func(t *testing.T) {
		got := functional.Left[string, int]("x").OrElse(other)
		if r, ok := got.Right(); !ok || r != 9 {
			t.Fatalf("OrElse.Right() = (%d, %t), want (9, true)", r, ok)
		}
	})
}

func TestEitherPeek(t *testing.T) {
	t.Run("runs f on a right and returns the receiver unchanged", func(t *testing.T) {
		seen := 0
		got := functional.Right[string](5).Peek(func(n int) { seen = n })
		if seen != 5 {
			t.Fatalf("Peek saw %d, want 5", seen)
		}
		if r, ok := got.Right(); !ok || r != 5 {
			t.Fatalf("Peek.Right() = (%d, %t), want (5, true)", r, ok)
		}
	})

	t.Run("skips f on a left", func(t *testing.T) {
		called := false
		got := functional.Left[string, int]("x").Peek(func(int) { called = true })
		if called {
			t.Fatalf("f was called on a left receiver")
		}
		if l, ok := got.Left(); !ok || l != "x" {
			t.Fatalf("Left() = (%q, %t), want (\"x\", true)", l, ok)
		}
	})
}

func TestEitherExists(t *testing.T) {
	isEven := func(n int) bool { return n%2 == 0 }

	t.Run("true for a right that passes", func(t *testing.T) {
		if !functional.Right[string](4).Exists(isEven) {
			t.Fatalf("Exists(isEven) = false, want true")
		}
	})

	t.Run("false for a right that fails", func(t *testing.T) {
		if functional.Right[string](3).Exists(isEven) {
			t.Fatalf("Exists(isEven) = true, want false")
		}
	})

	t.Run("false for a left without calling pred", func(t *testing.T) {
		called := false
		if functional.Left[string, int]("x").Exists(func(int) bool {
			called = true
			return true
		}) {
			t.Fatalf("Exists = true, want false")
		}
		if called {
			t.Fatalf("pred was called on a left receiver")
		}
	})
}

func TestEitherToOption(t *testing.T) {
	t.Run("a right becomes a present Option", func(t *testing.T) {
		o := functional.Right[string](5).ToOption()
		if v, ok := o.Get(); !ok || v != 5 {
			t.Fatalf("ToOption().Get() = (%d, %t), want (5, true)", v, ok)
		}
	})

	t.Run("a left becomes absence", func(t *testing.T) {
		o := functional.Left[string, int]("x").ToOption()
		if o.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
	})
}

func TestEitherToResult(t *testing.T) {
	t.Run("a right becomes a successful Result", func(t *testing.T) {
		v, err := functional.Right[string](5).ToResult(errors.New).Value()
		if err != nil || v != 5 {
			t.Fatalf("ToResult(...).Value() = (%d, %v), want (5, nil)", v, err)
		}
	})

	t.Run("a left becomes the error onLeft builds from it", func(t *testing.T) {
		sentinel := errors.New("unsupported payload")
		err := functional.Left[string, int]("legacy").ToResult(func(s string) error {
			return fmt.Errorf("%w: %s", sentinel, s)
		}).Err()
		if !errors.Is(err, sentinel) {
			t.Fatalf("Err() = %v, want it to wrap %v", err, sentinel)
		}
	})

	t.Run("onLeft is not called for a right", func(t *testing.T) {
		called := false
		functional.Right[string](5).ToResult(func(string) error {
			called = true
			return nil
		})
		if called {
			t.Fatalf("onLeft was called on a right receiver")
		}
	})
}

func ExampleEither() {
	classify := func(n int) functional.Either[string, int] {
		if n < 0 {
			return functional.Left[string, int]("negative input")
		}
		return functional.Right[string](n)
	}

	fmt.Println(classify(21).Map(func(n int) int { return n * 2 }).GetOrElse(0))
	fmt.Println(classify(-1).Fold(
		func(reason string) string { return "rejected: " + reason },
		func(n int) string { return fmt.Sprintf("accepted: %d", n) },
	))
	// Output:
	// 42
	// rejected: negative input
}
