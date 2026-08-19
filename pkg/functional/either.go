package functional

// Either carries a value that is exactly one of two possibilities — a left L
// or a right R. It exists for a genuine two-outcome branch where neither side
// is a failure: a parse that yields a cached hit or a fresh fetch, a
// discriminated union decoded from a webhook payload. Either is not this
// package's error type — Result[T] is. Reaching for Either[error, T] rebuilds
// Result without its error-translation methods; when one side means "it went
// wrong", use Result.
//
// Either is right-biased, following Scala 2.12+ and Vavr: Map and FlatMap
// operate on the right value, and a left passes through untouched. Pick the
// sides accordingly — the value the chain keeps working on goes right, the
// one that short-circuits goes left.
//
// The zero value (var e Either[L, R]) is a left carrying L's zero value. Go
// cannot forbid declaring one without a constructor, so the type defaults to
// the side the chain does not operate on: a forgotten Left/Right call can
// never fabricate a right value for Map or FlatMap to transform.
type Either[L, R any] struct {
	left    L
	right   R
	isRight bool
}

// Left builds an Either holding its left possibility — the side the
// right-biased Map and FlatMap pass through untouched.
func Left[L, R any](l L) Either[L, R] {
	return Either[L, R]{left: l}
}

// Right builds an Either holding its right possibility — the side Map and
// FlatMap operate on.
func Right[L, R any](r R) Either[L, R] {
	return Either[L, R]{right: r, isRight: true}
}

// IsLeft reports whether the left possibility is the one held.
func (e Either[L, R]) IsLeft() bool {
	return !e.isRight
}

// IsRight reports whether the right possibility is the one held.
func (e Either[L, R]) IsRight() bool {
	return e.isRight
}

// Left returns the left value and whether it is the side held, in Go's
// comma-ok shape — the counterpart of Option.Get for the side the chain
// short-circuits on.
func (e Either[L, R]) Left() (L, bool) {
	return e.left, !e.isRight
}

// Right returns the right value and whether it is the side held, in Go's
// comma-ok shape. It is the idiomatic way out of an Either for code that is
// done chaining:
//
//	if v, ok := e.Right(); ok {
//		use(v)
//	}
func (e Either[L, R]) Right() (R, bool) {
	return e.right, e.isRight
}

// Map transforms the right value, leaving a left untouched — the right bias
// in action. It is a generic method (Go 1.27) so f can map to a different
// type T than the receiver's R.
func (e Either[L, R]) Map[T any](f func(R) T) Either[L, T] {
	if !e.isRight {
		return Either[L, T]{left: e.left}
	}
	return Right[L](f(e.right))
}

// FlatMap chains a second Either-producing computation keyed by the right
// value, letting a left from either step end the chain the same way without
// an intermediate side check.
func (e Either[L, R]) FlatMap[T any](f func(R) Either[L, T]) Either[L, T] {
	if !e.isRight {
		return Either[L, T]{left: e.left}
	}
	return f(e.right)
}

// MapLeft transforms the left value, leaving a right untouched — for
// reshaping the short-circuit side without disturbing the chain.
func (e Either[L, R]) MapLeft[T any](f func(L) T) Either[T, R] {
	if e.isRight {
		return Right[T](e.right)
	}
	return Left[T, R](f(e.left))
}

// Fold collapses both possibilities into one value, forcing the caller to
// handle each side — the exhaustive way out of an Either, where GetOrElse
// discards what the left carries.
func (e Either[L, R]) Fold[T any](onLeft func(L) T, onRight func(R) T) T {
	if e.isRight {
		return onRight(e.right)
	}
	return onLeft(e.left)
}

// Swap exchanges the sides, turning a left into a right and back — for
// pointing the right-biased Map and FlatMap at what started as the left
// possibility.
func (e Either[L, R]) Swap() Either[R, L] {
	if e.isRight {
		return Left[R, L](e.right)
	}
	return Right[R](e.left)
}

// GetOrElse returns the right value, otherwise fallback — for a caller that
// only wants the right side and has no use for what the left carries.
func (e Either[L, R]) GetOrElse(fallback R) R {
	if e.isRight {
		return e.right
	}
	return fallback
}

// OrElse returns the receiver when it holds a right, otherwise other — the
// whole-Either counterpart of GetOrElse, keeping the chain alive instead of
// exiting it.
func (e Either[L, R]) OrElse(other Either[L, R]) Either[L, R] {
	if e.isRight {
		return e
	}
	return other
}

// Peek runs f against the right value for its side effect — logging, a
// metric — and returns the receiver unchanged. A left skips f entirely.
func (e Either[L, R]) Peek(f func(R)) Either[L, R] {
	if e.isRight {
		f(e.right)
	}
	return e
}

// Exists reports whether the right possibility is held and satisfies pred.
// A left is never a match, whatever pred would have said.
func (e Either[L, R]) Exists(pred func(R) bool) bool {
	return e.isRight && pred(e.right)
}

// ToOption keeps the right value as a present Option and turns a left into
// absence — not into failure, because a left is a legitimate outcome, not an
// error. What the left carries is discarded; use Fold or ToResult when it
// must survive the conversion.
func (e Either[L, R]) ToOption() Option[R] {
	if !e.isRight {
		return None[R]()
	}
	return Some(e.right)
}

// ToResult converts the left possibility into an error, collapsing the
// two-outcome branch into success-or-failure. The caller supplies what a
// left means as an error — mirroring Option.ToResult(absent) — because
// Either itself never decided that the left side was a failure:
//
//	cfg, err := decode(payload).ToResult(func(l LegacyPayload) error {
//		return fmt.Errorf("unsupported legacy payload %q", l.Kind)
//	}).Value()
func (e Either[L, R]) ToResult(onLeft func(L) error) Result[R] {
	if !e.isRight {
		return Failure[R](onLeft(e.left))
	}
	return Ok(e.right)
}
