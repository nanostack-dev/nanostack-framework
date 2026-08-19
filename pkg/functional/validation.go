package functional

import "errors"

// Validation carries either a value or every reason the value could not be
// produced. It exists for the one job Result is wrong for: validating a set
// of independent inputs. Result short-circuits — the first error ends the
// chain and nothing after it is even evaluated — which is correct for a
// pipeline where step two needs step one's value, and useless for a form
// where a user who submitted four bad fields must be told about all four,
// not just the first.
//
// Deliberately, Validation has no FlatMap. FlatMap is inherently sequential —
// f(a) cannot run until a exists — so a chain of FlatMaps can only ever
// report the first failure, which is the one thing this type exists to avoid.
// Accumulation instead happens through fixed-arity combination: the generated
// ZipValidationN family takes N Validations that already exist independently,
// so the errors of every invalid input can be collected, left to right. If
// you find yourself wanting FlatMap here, the steps depend on each other and
// Result is the right type.
//
// This is narrower than Vavr's Validation, which carries both a
// short-circuiting flatMap and an accumulating ap/combine and leaves the
// choice to the caller. Offering both on one type is a trap: reaching for
// FlatMap silently drops back to first-error reporting, which is the only
// reason anyone chose Validation over Result in the first place. One type,
// one behaviour.
type Validation[T any] struct {
	v    T
	errs []error
}

// errInvalidWithoutErrors stands in when Invalid is called with no non-nil
// errors. The alternatives are both worse: producing a valid Validation would
// let a code path that decided "invalid" report success, and panicking would
// let a slice-plumbing bug take down a request that was merely being
// validated. Substituting a descriptive error keeps the constructor honest —
// Invalid always yields an invalid Validation — and fails in the safe
// direction.
var errInvalidWithoutErrors = errors.New(
	"functional: Invalid constructed with no non-nil errors",
)

// Valid builds a valid Validation carrying v.
func Valid[T any](v T) Validation[T] {
	return Validation[T]{v: v}
}

// Invalid builds an invalid Validation carrying every non-nil error given.
// Nil errors are dropped — they carry no reason and would make errors.Join
// misbehave. When nothing non-nil remains (including a bare Invalid() call),
// the result still reports invalid, carrying errInvalidWithoutErrors: a
// constructor named Invalid must never hand back a Validation that claims to
// be valid. The variadic slice is filtered into a fresh slice, so a caller
// passing errs... keeps ownership of its own slice.
func Invalid[T any](errs ...error) Validation[T] {
	return Validation[T]{errs: normalizeErrs(errs)}
}

// IsValid reports whether a value is here rather than errors. Unlike Option
// there is no third state to distinguish — a Validation is exactly one of
// valid or invalid.
func (v Validation[T]) IsValid() bool {
	return len(v.errs) == 0
}

// Value returns the value. Its result is meaningful only when IsValid is
// true; otherwise it is T's zero value.
func (v Validation[T]) Value() T {
	return v.v
}

// Errors returns the accumulated errors, in the order they were collected,
// or nil when valid. The slice is a fresh copy on every call — the internal
// slice is never handed out, so a caller sorting or truncating the result
// cannot corrupt the Validation it came from.
func (v Validation[T]) Errors() []error {
	if len(v.errs) == 0 {
		return nil
	}
	errs := make([]error, len(v.errs))
	copy(errs, v.errs)
	return errs
}

// Err collapses the accumulated errors into one via errors.Join, or returns
// nil when valid — so a Validation can leave this package as an ordinary Go
// error. errors.Join preserves the tree: errors.Is against the result still
// matches every individual accumulated error, and errors.As still finds each
// one's type.
func (v Validation[T]) Err() error {
	return errors.Join(v.errs...)
}

// Map transforms the value when valid, leaving an invalid Validation's
// errors untouched. It is a generic method (Go 1.27) so f can map to a
// different type R than the receiver's T.
func (v Validation[T]) Map[R any](f func(T) R) Validation[R] {
	if len(v.errs) > 0 {
		return Validation[R]{errs: v.errs}
	}
	return Valid(f(v.v))
}

// MapErrors transforms the accumulated errors when invalid, leaving a valid
// Validation untouched — for wrapping each error with the field or section
// it belongs to before the list leaves a boundary. f receives a copy, and
// its result is filtered like Invalid's input: nil errors are dropped, and
// an empty result still reports invalid, carrying errInvalidWithoutErrors —
// MapErrors can reshape the reasons, never erase the verdict.
func (v Validation[T]) MapErrors(f func([]error) []error) Validation[T] {
	if len(v.errs) == 0 {
		return v
	}
	return Validation[T]{errs: normalizeErrs(f(v.Errors()))}
}

// ToResult converts to a Result, joining the accumulated errors via Err. Use
// it at the point where validation is over and the caller only needs Go's
// two-state success-or-failure shape; the individual errors stay reachable
// through errors.Is and errors.As on the joined error.
func (v Validation[T]) ToResult() Result[T] {
	if len(v.errs) > 0 {
		return Failure[T](v.Err())
	}
	return Ok(v.v)
}

// ToOption converts to an Option, carrying the joined errors as a failure.
// It never produces legitimate absence — invalid is a failure, not a missing
// value, so IsPresent-only callers do not mistake a rejected input for an
// empty lookup.
func (v Validation[T]) ToOption() Option[T] {
	if len(v.errs) > 0 {
		return Failed[T](v.Err())
	}
	return Some(v.v)
}

// normalizeErrs is the single gate through which an invalid Validation's
// error slice is built: nil errors dropped, a fresh slice always allocated,
// and never empty — an empty result would let an "invalid" value claim to be
// valid, the one state this type must make unrepresentable.
func normalizeErrs(errs []error) []error {
	kept := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			kept = append(kept, err)
		}
	}
	if len(kept) == 0 {
		return []error{errInvalidWithoutErrors}
	}
	return kept
}

// collectErrs concatenates the inputs' error slices left to right, returning
// nil when every input was valid. The generated ZipValidation helpers use it
// to accumulate — where firstErr keeps only the leftmost error for the
// short-circuiting Zip families, collectErrs keeps them all, in argument
// order, so a caller can render field errors in the order the fields appear
// on the form.
func collectErrs(errLists ...[]error) []error {
	var collected []error
	for _, errs := range errLists {
		collected = append(collected, errs...)
	}
	return collected
}
