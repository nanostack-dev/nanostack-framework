package functional

import "fmt"

// Result carries a value that may have failed. Unlike Option there is no
// third "legitimately absent" state — a Result always has either its value
// or its error, never neither.
type Result[T any] struct {
	v   T
	err error
}

// New builds a Result from a (value, error) pair — the shape most Go
// functions already return, and the shape Value() unwraps back into. Use
// this to lift an existing (T, error)-returning call straight into a Result,
// or when a value must survive alongside an error so a later step (such as
// Result's own SQL error translation) can still see it.
func New[T any](v T, err error) Result[T] {
	return Result[T]{v: v, err: err}
}

// Ok builds a successful Result carrying v.
func Ok[T any](v T) Result[T] {
	return Result[T]{v: v}
}

// Failure builds a failed Result carrying err, with T's zero value. Named
// Failure rather than Err so it does not collide with the Err() method every
// Result carries.
func Failure[T any](err error) Result[T] {
	return Result[T]{err: err}
}

// IsOk reports whether the computation succeeded.
func (r Result[T]) IsOk() bool {
	return r.err == nil
}

// Value returns the value and the error together, so a caller cannot forget
// to check one while reading the other.
func (r Result[T]) Value() (T, error) {
	return r.v, r.err
}

// Err returns only the error, for computations whose value carries no
// information.
func (r Result[T]) Err() error {
	return r.err
}

// Map transforms the value on success, leaving a failure untouched. It is a
// generic method (Go 1.27) so f can map to a different type R than the
// receiver's T.
func (r Result[T]) Map[R any](f func(T) R) Result[R] {
	if r.err != nil {
		return Result[R]{err: r.err}
	}
	return Ok(f(r.v))
}

// FlatMap chains a second Result-producing computation, letting a failure
// from either step propagate without an intermediate error check.
func (r Result[T]) FlatMap[R any](f func(T) Result[R]) Result[R] {
	if r.err != nil {
		return Result[R]{err: r.err}
	}
	return f(r.v)
}

// MapErr transforms the error on failure, leaving success untouched. This is
// the general shape transactor's OnUnique/OnForeignKey/OnSQLState/OnNoRows
// build on to translate a driver error into a domain sentinel.
func (r Result[T]) MapErr(f func(error) error) Result[T] {
	if r.err == nil {
		return r
	}
	return Result[T]{v: r.v, err: f(r.err)}
}

// OrElse returns the value on success, otherwise fallback — for a caller that
// wants a usable value and has no interest in why the computation failed.
func (r Result[T]) OrElse(fallback T) T {
	if r.err != nil {
		return fallback
	}
	return r.v
}

// ToOption converts a failure into an Option carrying that same failure, and a
// success into a present one. It is the inverse direction of Option.ToResult,
// for feeding a Result into a chain that continues in Option's shape.
//
// It never produces legitimate absence: a Result has no third state to map
// onto None, so only Option.Filter or an Option-returning FlatMap can
// introduce absence downstream.
func (r Result[T]) ToOption() Option[T] {
	if r.err != nil {
		return Failed[T](r.err)
	}
	return Some(r.v)
}

// Try lifts a (T, error)-returning call into a Result — Vavr's Try.of, in
// the shape Go functions already have. It is New for a call site rather than
// a pair the caller already holds, so a chain can start directly from the
// call:
//
//	cfg := functional.Try(loadConfig).Map(normalize)
//
// Like New, it keeps whatever value f returned alongside a non-nil error, so
// a later step can still see it.
func Try[T any](f func() (T, error)) Result[T] {
	return New(f())
}

// TryRecover is Try with a safety net: a panic inside f becomes a failed
// Result instead of unwinding the caller. Reach for it only at a boundary
// where someone else's code runs under yours — a goroutine entry point whose
// crash would take the whole process down, a plugin or user-supplied
// callback, a worker loop that must outlive one bad job. It is not general
// control flow: inside code you own, a panic is a programmer bug, and Try's
// crash is the diagnostic you want, stack intact.
//
// A recovered error value stays errors.Is/errors.As-transparent inside the
// returned failure. Any other panic value is rendered into the error
// message. The panicking stack is gone by the time the caller reads the
// Result, so log at the recovery site if the trace matters.
func TryRecover[T any](f func() (T, error)) (r Result[T]) {
	defer func() {
		if p := recover(); p != nil {
			if err, ok := p.(error); ok {
				r = Failure[T](fmt.Errorf("recovered panic: %w", err))
				return
			}
			r = Failure[T](fmt.Errorf("recovered panic: %v", p))
		}
	}()
	return New(f())
}

// Recover turns a failure into a success by computing a replacement value
// from the error — Vavr's Try.recover. Unlike OrElseGet it stays inside the
// Result, so the chain can keep going. A success passes through with f never
// called.
func (r Result[T]) Recover(f func(error) T) Result[T] {
	if r.err == nil {
		return r
	}
	return Ok(f(r.err))
}

// RecoverWith is Recover for a fallback that can itself fail — Vavr's
// Try.recoverWith. Use it when the recovery is another fallible computation,
// such as a second data source or a retry against a replica, rather than a
// value the caller can simply produce.
func (r Result[T]) RecoverWith(f func(error) Result[T]) Result[T] {
	if r.err == nil {
		return r
	}
	return f(r.err)
}

// Fold collapses the Result into a single value by handling both states at
// once — the one exit that makes forgetting a branch impossible, because the
// compiler demands both functions. It is a generic method (Go 1.27) so the
// folded-to type R is the caller's choice.
func (r Result[T]) Fold[R any](onErr func(error) R, onOk func(T) R) R {
	if r.err != nil {
		return onErr(r.err)
	}
	return onOk(r.v)
}

// Filter turns a success that fails pred into a failure carrying err. The
// caller supplies the error because only the caller knows what the predicate
// means — the same reason Option.ToResult takes its absence error from the
// caller. The rejected value stays alongside the error, as with New, so a
// later step can still see what was rejected. An existing failure is
// unchanged: pred never runs, and the original error is not replaced.
func (r Result[T]) Filter(pred func(T) bool, err error) Result[T] {
	if r.err != nil || pred(r.v) {
		return r
	}
	return Result[T]{v: r.v, err: err}
}

// Peek runs a side effect on the value of a success and returns the receiver
// unchanged — for logging or metrics inside a chain without breaking it. A
// failure passes through with f never called.
func (r Result[T]) Peek(f func(T)) Result[T] {
	if r.err == nil {
		f(r.v)
	}
	return r
}

// PeekErr is Peek for the failure side: it runs a side effect on the error
// and returns the receiver unchanged. A success passes through with f never
// called.
func (r Result[T]) PeekErr(f func(error)) Result[T] {
	if r.err != nil {
		f(r.err)
	}
	return r
}

// OrElseGet is OrElse with the fallback computed only when it is needed, and
// handed the error so the fallback can depend on why the computation failed.
// For a constant default, OrElse reads better.
func (r Result[T]) OrElseGet(f func(error) T) T {
	if r.err != nil {
		return f(r.err)
	}
	return r.v
}

// Must returns the value or panics on failure — the regexp.MustCompile
// pattern, for package-level variables and test fixtures where a failure is
// a programmer error and crashing at startup is the correct response. It is
// bluntly not for request paths or any code handling runtime input: there,
// unwrap with Value and return the error. The panic value wraps the original
// error, so a recover site can still errors.Is it.
func (r Result[T]) Must() T {
	if r.err != nil {
		panic(fmt.Errorf("functional: Must on failed Result: %w", r.err))
	}
	return r.v
}
