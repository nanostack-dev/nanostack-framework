package functional_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestLazy(t *testing.T) {
	t.Run("supplier does not run before the first Get", func(t *testing.T) {
		var calls atomic.Int64
		l := functional.NewLazy(func() int {
			calls.Add(1)
			return 42
		})
		if got := calls.Load(); got != 0 {
			t.Fatalf("supplier calls before Get = %d, want 0", got)
		}
		if l.IsEvaluated() {
			t.Fatalf("IsEvaluated() = true before Get, want false")
		}
	})

	t.Run("supplier runs exactly once across repeated Gets", func(t *testing.T) {
		var calls atomic.Int64
		l := functional.NewLazy(func() int {
			calls.Add(1)
			return 42
		})
		for range 5 {
			if got := l.Get(); got != 42 {
				t.Fatalf("Get() = %d, want 42", got)
			}
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("supplier calls after 5 Gets = %d, want 1", got)
		}
	})

	t.Run("IsEvaluated transitions from false to true on first Get", func(t *testing.T) {
		l := functional.NewLazy(func() string { return "value" })
		if l.IsEvaluated() {
			t.Fatalf("IsEvaluated() = true before Get, want false")
		}
		if got := l.Get(); got != "value" {
			t.Fatalf("Get() = %q, want %q", got, "value")
		}
		if !l.IsEvaluated() {
			t.Fatalf("IsEvaluated() = false after Get, want true")
		}
	})
}

func TestLazyOf(t *testing.T) {
	t.Run("is evaluated from the start", func(t *testing.T) {
		l := functional.LazyOf(7)
		if !l.IsEvaluated() {
			t.Fatalf("IsEvaluated() = false, want true")
		}
		if got := l.Get(); got != 7 {
			t.Fatalf("Get() = %d, want 7", got)
		}
	})

	t.Run("repeated Gets keep returning the value", func(t *testing.T) {
		l := functional.LazyOf("value")
		for range 3 {
			if got := l.Get(); got != "value" {
				t.Fatalf("Get() = %q, want %q", got, "value")
			}
		}
	})
}

func TestLazyMap(t *testing.T) {
	t.Run("does not force the receiver", func(t *testing.T) {
		var calls atomic.Int64
		l := functional.NewLazy(func() int {
			calls.Add(1)
			return 21
		})
		mapped := l.Map(func(n int) int { return n * 2 })
		if got := calls.Load(); got != 0 {
			t.Fatalf("supplier calls after Map = %d, want 0 — Map must stay lazy", got)
		}
		if l.IsEvaluated() {
			t.Fatalf("receiver IsEvaluated() = true after Map, want false")
		}
		if mapped.IsEvaluated() {
			t.Fatalf("mapped IsEvaluated() = true before Get, want false")
		}
	})

	t.Run("first Get on the mapped Lazy forces both, once each", func(t *testing.T) {
		var calls atomic.Int64
		l := functional.NewLazy(func() int {
			calls.Add(1)
			return 21
		})
		mapped := l.Map(func(n int) int { return n * 2 })
		if got := mapped.Get(); got != 42 {
			t.Fatalf("mapped.Get() = %d, want 42", got)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("supplier calls = %d, want 1", got)
		}
		if !l.IsEvaluated() {
			t.Fatalf("receiver IsEvaluated() = false after mapped Get, want true")
		}
	})

	t.Run("changes type, not just value", func(t *testing.T) {
		l := functional.LazyOf(7)
		mapped := l.Map(func(n int) bool { return n > 0 })
		if got := mapped.Get(); got != true {
			t.Fatalf("mapped.Get() = %t, want true", got)
		}
	})

	t.Run("f runs once even when the mapped Lazy is read twice", func(t *testing.T) {
		var fCalls atomic.Int64
		mapped := functional.LazyOf(3).Map(func(n int) int {
			fCalls.Add(1)
			return n + 1
		})
		if got := mapped.Get(); got != 4 {
			t.Fatalf("mapped.Get() = %d, want 4", got)
		}
		if got := mapped.Get(); got != 4 {
			t.Fatalf("second mapped.Get() = %d, want 4", got)
		}
		if got := fCalls.Load(); got != 1 {
			t.Fatalf("f calls = %d, want 1", got)
		}
	})
}

func TestLazyConcurrentGet(t *testing.T) {
	const goroutines = 200

	var calls atomic.Int64
	l := functional.NewLazy(func() int {
		calls.Add(1)
		return 42
	})

	var start sync.WaitGroup
	start.Add(1)
	values := make([]int, goroutines)
	var done sync.WaitGroup
	for i := range goroutines {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			values[i] = l.Get()
		}()
	}
	start.Done()
	done.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("supplier calls under %d concurrent Gets = %d, want 1", goroutines, got)
	}
	for i, v := range values {
		if v != 42 {
			t.Fatalf("goroutine %d observed %d, want 42", i, v)
		}
	}
	if !l.IsEvaluated() {
		t.Fatalf("IsEvaluated() = false after concurrent Gets, want true")
	}
}
