package functional

import (
	"sync"
	"sync/atomic"
)

// Lazy carries a value computed at most once, on first access, then
// memoized — Vavr's Lazy, for a value that is expensive to produce and may
// never be needed. Concurrent Get calls are safe: the supplier runs exactly
// once, and every caller observes the same value.
//
// A Lazy is always handled through a pointer — NewLazy and LazyOf return
// *Lazy[T], and every method has a pointer receiver, because the struct
// embeds a sync.Once that must not be copied after first use. Keeping the
// pointer shape end to end means ordinary use never dereferences into a
// copy, and go vet's copylocks check flags any code that does.
//
// There is no failure-carrying constructor: a supplier that can fail is
// already expressible as Lazy[Result[T]] via NewLazy(func() Result[T] {...}),
// which memoizes the failure exactly like a value and keeps this type to a
// single concern. A dedicated NewLazyResult would only restate that
// composition under a second name.
//
// It is not sync.OnceValue with extra steps. OnceValue memoizes one call and
// hands back a func() T, which composes only by wrapping — f(g()) re-runs f
// on every read, because the outer layer keeps no memo of its own. Map here
// returns a Lazy whose own result is memoized too, so a chain of derivations
// costs one evaluation per link rather than one per read. Reach for
// sync.OnceValue when nothing derives from the value; reach for Lazy when
// something does.
type Lazy[T any] struct {
	once      sync.Once
	evaluated atomic.Bool
	supplier  func() T
	v         T
}

// NewLazy builds a Lazy whose supplier runs on the first Get — not before,
// not again. The supplier reference is dropped once it has run, so a closure
// capturing something large does not keep it reachable for the Lazy's whole
// lifetime.
func NewLazy[T any](supplier func() T) *Lazy[T] {
	return &Lazy[T]{supplier: supplier}
}

// LazyOf builds an already-evaluated Lazy carrying v — for handing a value
// that is already in hand to an API that wants a Lazy, without wrapping it
// in a supplier that would pointlessly defer nothing.
func LazyOf[T any](v T) *Lazy[T] {
	l := &Lazy[T]{v: v}
	l.once.Do(func() {})
	l.evaluated.Store(true)
	return l
}

// Get returns the value, running the supplier first if no call has yet — the
// sync.Once guarantees exactly-once evaluation and publishes the value to
// every concurrent caller.
func (l *Lazy[T]) Get() T {
	l.once.Do(func() {
		l.v = l.supplier()
		l.supplier = nil
		l.evaluated.Store(true)
	})
	return l.v
}

// IsEvaluated reports whether the value has been computed, without forcing
// it — for a caller deciding whether reading the value now would pay the
// supplier's cost. It is a point-in-time observation: another goroutine may
// evaluate the Lazy immediately after a false answer.
func (l *Lazy[T]) IsEvaluated() bool {
	return l.evaluated.Load()
}

// Map derives a new Lazy whose value is f applied to this one's. It stays
// lazy in both directions: neither the receiver nor the derived Lazy is
// forced until the derived one's first Get, so a chain of Maps costs nothing
// until something at the end is actually read. It is a generic method
// (Go 1.27) so f can map to a different type R than the receiver's T.
func (l *Lazy[T]) Map[R any](f func(T) R) *Lazy[R] {
	return NewLazy(func() R {
		return f(l.Get())
	})
}
