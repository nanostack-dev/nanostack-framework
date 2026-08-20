package functional_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
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
		got := functional.Failure[int](sentinel).FlatMap(func(_ int) functional.Result[string] {
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
		got := functional.Ok(1).FlatMap(func(_ int) functional.Result[string] {
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

func TestResultOrElse(t *testing.T) {
	t.Run("returns the value on success", func(t *testing.T) {
		if got := functional.Ok(5).OrElse(9); got != 5 {
			t.Fatalf("OrElse(9) = %d, want 5", got)
		}
	})

	t.Run("returns the fallback on failure", func(t *testing.T) {
		if got := functional.Failure[int](errors.New("boom")).OrElse(9); got != 9 {
			t.Fatalf("OrElse(9) = %d, want 9", got)
		}
	})
}

func TestResultToOption(t *testing.T) {
	t.Run("success becomes a present Option", func(t *testing.T) {
		got := functional.Ok(5).ToOption()
		if !got.IsPresent() || got.Value() != 5 {
			t.Fatalf("ToOption() = %+v, want present 5", got)
		}
	})

	t.Run("failure carries the error into the Option", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Failure[int](sentinel).ToOption()
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("round-trips back through ToResult", func(t *testing.T) {
		// The two bridges are inverses on the states they share: a success
		// survives the trip, and so does a real failure.
		errAbsent := errors.New("not found")
		v, err := functional.Ok(5).ToOption().ToResult(errAbsent).Value()
		if err != nil || v != 5 {
			t.Fatalf("round trip = (%d, %v), want (5, nil)", v, err)
		}

		sentinel := errors.New("boom")
		err = functional.Failure[int](sentinel).ToOption().ToResult(errAbsent).Err()
		if !errors.Is(err, sentinel) {
			t.Fatalf("round trip err = %v, want %v", err, sentinel)
		}
	})
}

func TestResultTry(t *testing.T) {
	t.Run("lifts a successful call", func(t *testing.T) {
		got := functional.Try(func() (int, error) { return 42, nil })
		v, err := got.Value()
		if err != nil || v != 42 {
			t.Fatalf("Try().Value() = (%d, %v), want (42, nil)", v, err)
		}
	})

	t.Run("lifts a failed call with errors.Is transparency", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Try(func() (int, error) { return 0, sentinel })
		if got.IsOk() {
			t.Fatalf("IsOk() = true, want false")
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("keeps the value that arrived alongside the error", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.Try(func() (int, error) { return 7, sentinel })
		v, err := got.Value()
		if !errors.Is(err, sentinel) || v != 7 {
			t.Fatalf("Value() = (%d, %v), want (7, %v)", v, err, sentinel)
		}
	})
}

func TestResultTryRecover(t *testing.T) {
	t.Run("passes a successful call through", func(t *testing.T) {
		got := functional.TryRecover(func() (int, error) { return 42, nil })
		v, err := got.Value()
		if err != nil || v != 42 {
			t.Fatalf("TryRecover().Value() = (%d, %v), want (42, nil)", v, err)
		}
	})

	t.Run("passes an ordinary error through with errors.Is transparency", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := functional.TryRecover(func() (int, error) { return 7, sentinel })
		v, err := got.Value()
		if !errors.Is(err, sentinel) || v != 7 {
			t.Fatalf("Value() = (%d, %v), want (7, %v)", v, err, sentinel)
		}
	})

	t.Run("a panicked error stays errors.Is-matchable", func(t *testing.T) {
		sentinel := errors.New("panicked sentinel")
		got := functional.TryRecover(func() (int, error) { panic(sentinel) })
		if got.IsOk() {
			t.Fatalf("IsOk() = true, want false")
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want it to wrap %v", got.Err(), sentinel)
		}
	})

	t.Run("a panicked string is legible in the error", func(t *testing.T) {
		got := functional.TryRecover(func() (int, error) { panic("wires crossed") })
		if got.IsOk() {
			t.Fatalf("IsOk() = true, want false")
		}
		if !strings.Contains(got.Err().Error(), "wires crossed") {
			t.Fatalf("Err() = %q, want it to contain %q", got.Err().Error(), "wires crossed")
		}
	})

	t.Run("a nil-map-write runtime panic becomes a runtime.Error failure", func(t *testing.T) {
		got := functional.TryRecover(func() (int, error) {
			m := nilMap()
			m["k"] = 1
			return m["k"], nil
		})
		if got.IsOk() {
			t.Fatalf("IsOk() = true, want false")
		}
		var rerr runtime.Error
		if !errors.As(got.Err(), &rerr) {
			t.Fatalf("Err() = %v, want it to wrap a runtime.Error", got.Err())
		}
	})
}

func TestResultRecover(t *testing.T) {
	t.Run("turns a failure into a success computed from the error", func(t *testing.T) {
		got := functional.Failure[string](errors.New("boom")).Recover(func(err error) string {
			return "fallback for " + err.Error()
		})
		v, err := got.Value()
		if err != nil || v != "fallback for boom" {
			t.Fatalf("Recover().Value() = (%q, %v), want (\"fallback for boom\", nil)", v, err)
		}
	})

	t.Run("leaves a success untouched without calling f", func(t *testing.T) {
		called := false
		got := functional.Ok(5).Recover(func(error) int {
			called = true
			return 9
		})
		if called {
			t.Fatalf("f was called on a successful receiver")
		}
		if v, err := got.Value(); err != nil || v != 5 {
			t.Fatalf("Value() = (%d, %v), want (5, nil)", v, err)
		}
	})
}

func TestResultRecoverWith(t *testing.T) {
	t.Run("replaces a failure with the fallback's success", func(t *testing.T) {
		got := functional.Failure[int](errors.New("boom")).RecoverWith(func(error) functional.Result[int] {
			return functional.Ok(9)
		})
		v, err := got.Value()
		if err != nil || v != 9 {
			t.Fatalf("RecoverWith().Value() = (%d, %v), want (9, nil)", v, err)
		}
	})

	t.Run("the fallback's own failure propagates", func(t *testing.T) {
		second := errors.New("second boom")
		got := functional.Failure[int](errors.New("boom")).RecoverWith(func(error) functional.Result[int] {
			return functional.Failure[int](second)
		})
		if !errors.Is(got.Err(), second) {
			t.Fatalf("Err() = %v, want %v", got.Err(), second)
		}
	})

	t.Run("leaves a success untouched without calling f", func(t *testing.T) {
		called := false
		got := functional.Ok(5).RecoverWith(func(error) functional.Result[int] {
			called = true
			return functional.Ok(9)
		})
		if called {
			t.Fatalf("f was called on a successful receiver")
		}
		if v, err := got.Value(); err != nil || v != 5 {
			t.Fatalf("Value() = (%d, %v), want (5, nil)", v, err)
		}
	})
}

func TestResultFold(t *testing.T) {
	toLabel := func(n int) string { return fmt.Sprintf("ok:%d", n) }
	toMessage := func(err error) string { return "err:" + err.Error() }

	t.Run("folds a success through onOk", func(t *testing.T) {
		if got := functional.Ok(42).Fold(toMessage, toLabel); got != "ok:42" {
			t.Fatalf("Fold() = %q, want %q", got, "ok:42")
		}
	})

	t.Run("folds a failure through onErr", func(t *testing.T) {
		got := functional.Failure[int](errors.New("boom")).Fold(toMessage, toLabel)
		if got != "err:boom" {
			t.Fatalf("Fold() = %q, want %q", got, "err:boom")
		}
	})
}

func TestResultFilter(t *testing.T) {
	positive := func(n int) bool { return n > 0 }

	t.Run("a success passing the predicate is unchanged", func(t *testing.T) {
		got := functional.Ok(5).Filter(positive, errors.New("unused"))
		if v, err := got.Value(); err != nil || v != 5 {
			t.Fatalf("Value() = (%d, %v), want (5, nil)", v, err)
		}
	})

	t.Run("a success failing the predicate carries the supplied error and keeps the value", func(t *testing.T) {
		sentinel := errors.New("not positive")
		got := functional.Ok(-3).Filter(positive, sentinel)
		v, err := got.Value()
		if !errors.Is(err, sentinel) {
			t.Fatalf("Err() = %v, want %v", err, sentinel)
		}
		if v != -3 {
			t.Fatalf("Value() = %d, want -3 — the rejected value must survive", v)
		}
	})

	t.Run("an existing failure is unchanged and pred never runs", func(t *testing.T) {
		original := errors.New("boom")
		called := false
		got := functional.Failure[int](original).Filter(func(int) bool {
			called = true
			return true
		}, errors.New("replacement"))
		if called {
			t.Fatalf("pred was called on a failed receiver")
		}
		if !errors.Is(got.Err(), original) {
			t.Fatalf("Err() = %v, want the original %v", got.Err(), original)
		}
	})
}

func TestResultPeek(t *testing.T) {
	t.Run("runs the side effect on a success and returns the receiver unchanged", func(t *testing.T) {
		seen := 0
		got := functional.Ok(5).Peek(func(n int) { seen = n })
		if seen != 5 {
			t.Fatalf("seen = %d, want 5", seen)
		}
		if v, err := got.Value(); err != nil || v != 5 {
			t.Fatalf("Value() = (%d, %v), want (5, nil)", v, err)
		}
	})

	t.Run("skips the side effect on a failure", func(t *testing.T) {
		sentinel := errors.New("boom")
		called := false
		got := functional.Failure[int](sentinel).Peek(func(int) { called = true })
		if called {
			t.Fatalf("f was called on a failed receiver")
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})
}

func TestResultPeekErr(t *testing.T) {
	t.Run("runs the side effect on a failure and returns the receiver unchanged", func(t *testing.T) {
		sentinel := errors.New("boom")
		var seen error
		got := functional.Failure[int](sentinel).PeekErr(func(err error) { seen = err })
		if !errors.Is(seen, sentinel) {
			t.Fatalf("seen = %v, want %v", seen, sentinel)
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("skips the side effect on a success", func(t *testing.T) {
		called := false
		got := functional.Ok(5).PeekErr(func(error) { called = true })
		if called {
			t.Fatalf("f was called on a successful receiver")
		}
		if v, err := got.Value(); err != nil || v != 5 {
			t.Fatalf("Value() = (%d, %v), want (5, nil)", v, err)
		}
	})
}

func TestResultOrElseGet(t *testing.T) {
	t.Run("returns the value on success without calling f", func(t *testing.T) {
		called := false
		got := functional.Ok(5).OrElseGet(func(error) int {
			called = true
			return 9
		})
		if called {
			t.Fatalf("f was called on a successful receiver")
		}
		if got != 5 {
			t.Fatalf("OrElseGet() = %d, want 5", got)
		}
	})

	t.Run("computes the fallback from the error on failure", func(t *testing.T) {
		sentinel := errors.New("boom")
		var seen error
		got := functional.Failure[int](sentinel).OrElseGet(func(err error) int {
			seen = err
			return 9
		})
		if got != 9 {
			t.Fatalf("OrElseGet() = %d, want 9", got)
		}
		if !errors.Is(seen, sentinel) {
			t.Fatalf("fallback saw %v, want %v", seen, sentinel)
		}
	})
}

func TestResultMust(t *testing.T) {
	t.Run("returns the value on success", func(t *testing.T) {
		if got := functional.Ok(5).Must(); got != 5 {
			t.Fatalf("Must() = %d, want 5", got)
		}
	})

	t.Run("panics on failure with an errors.Is-matchable error", func(t *testing.T) {
		sentinel := errors.New("boom")
		defer func() {
			p := recover()
			if p == nil {
				t.Fatalf("Must() did not panic on a failed Result")
			}
			err, ok := p.(error)
			if !ok {
				t.Fatalf("panic value = %v (%T), want an error", p, p)
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("panic error = %v, want it to wrap %v", err, sentinel)
			}
		}()
		functional.Failure[int](sentinel).Must()
	})
}

// nilMap returns a nil map through a call boundary. Writing to it panics at
// run time, which is what the TryRecover test needs, but govet's nilness pass
// cannot prove the nil across the call and so does not report the write.
func nilMap() map[string]int {
	return nil
}
