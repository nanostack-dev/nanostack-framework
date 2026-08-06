// Package functional provides small, generic value types for chaining
// computations that may be absent or may fail — Option[T] (a value that may
// legitimately be absent), Result[T] (a value that may have failed), and
// Tuple2/Tuple3 (fixed-arity value grouping, for a step that needs to pass
// more than one value to the next).
//
// Go has no higher-kinded types, so there is no shared Functor/Monad
// interface to implement once — Option and Result each get their own
// hand-written Map/FlatMap. What Go 1.27 does add is generic methods
// (go.dev/issue/77273): Map/FlatMap can be real methods on Option[T] and
// Result[T] with their own type parameter for the target type, giving
// Java-Stream-style fluent chaining (opt.Map(f).Filter(pred).OrElse(v))
// instead of the package-level helper functions older Go required — the
// shape samber/mo (predating generic methods) still uses.
package functional

// Option carries a value that may legitimately be absent — as opposed to a
// failure. A SELECT matching zero rows is the paradigm case: not every
// "no value" is an error.
//
// Some, None, and Failed are the only ways to build one, so the states
// IsPresent distinguishes (present, absent, failed) are the only ones
// reachable — there is no way to construct a value that is simultaneously
// "present" and "failed".
type Option[T any] struct {
	v       T
	present bool
	err     error
}

// Some builds a present Option carrying v.
func Some[T any](v T) Option[T] {
	return Option[T]{v: v, present: true}
}

// None builds an absent Option: no value, no error.
func None[T any]() Option[T] {
	return Option[T]{}
}

// Failed builds an Option carrying err. IsPresent is false regardless of the
// zero value's shape — a failure is never mistaken for legitimate absence.
func Failed[T any](err error) Option[T] {
	return Option[T]{err: err}
}

// IsPresent reports whether a value is here. It is false both when the
// value is legitimately absent (Err is nil) and when the computation failed
// (Err is non-nil) — check Err first to tell those apart.
func (o Option[T]) IsPresent() bool {
	return o.err == nil && o.present
}

// Err returns the failure, if any. Legitimate absence does not set this.
func (o Option[T]) Err() error {
	return o.err
}

// Value returns the value. Its result is meaningful only when IsPresent is
// true; otherwise it is T's zero value.
func (o Option[T]) Value() T {
	return o.v
}

// Map transforms the value when present, leaving absence and failure
// untouched. It is a generic method (Go 1.27) so f can map to a different
// type R than the receiver's T.
func (o Option[T]) Map[R any](f func(T) R) Option[R] {
	if !o.IsPresent() {
		return Option[R]{err: o.err}
	}
	return Some(f(o.v))
}

// FlatMap chains a second Option-producing computation keyed by the present
// value, letting absence or failure from either step propagate without an
// intermediate presence check. Use this to look up A, then look up B keyed
// by A, where absence from either lookup should end the chain the same way.
func (o Option[T]) FlatMap[R any](f func(T) Option[R]) Option[R] {
	if !o.IsPresent() {
		return Option[R]{err: o.err}
	}
	return f(o.v)
}

// Filter turns a present value that fails pred into absence. An existing
// failure or absence is unchanged.
func (o Option[T]) Filter(pred func(T) bool) Option[T] {
	if !o.IsPresent() || pred(o.v) {
		return o
	}
	return Option[T]{err: o.err}
}

// OrElse returns the value if present, otherwise fallback. It does not
// distinguish absence from failure — a caller that must react to a failure
// differently from legitimate absence should check Err first.
func (o Option[T]) OrElse(fallback T) T {
	if o.IsPresent() {
		return o.v
	}
	return fallback
}
