package functional

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
