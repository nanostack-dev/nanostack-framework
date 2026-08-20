// Package functional provides small, generic value types for chaining
// computations that may be absent or may fail — Option[T] (a value that may
// legitimately be absent), Result[T] (a value that may have failed), and
// Tuple2..Tuple9 (fixed-arity value grouping, for a step that needs to pass
// more than one value to the next).
//
// The distinction the package exists for is that absent is not failed: a
// SELECT matching zero rows is success carrying no row. The two are kept
// apart by type rather than by convention. Option has exactly two states and
// carries no error, so a function that can both fail and find nothing returns
// (Option[T], error) — the shape Go already uses, and the one errcheck can
// see. An error hidden inside a value is invisible to the compiler and to
// every linter, which is why Option does not hold one.
//
// Go has no higher-kinded types, so there is no shared Functor/Monad
// interface to implement once — Option and Result each get their own
// hand-written Map/FlatMap. What Go 1.27 does add is generic methods
// (go.dev/issue/77273): Map/FlatMap can be real methods on Option[T] and
// Result[T] with their own type parameter for the target type, giving
// Java-Stream-style fluent chaining (opt.Map(f).Filter(pred).OrElse(v))
// instead of the package-level helper functions older Go required — the
// shape samber/mo (predating generic methods) still uses.
//
// Go also has no variadic generics, so the fixed-arity members — Tuple2..
// Tuple9 and the ZipOption/ZipResult families that build them — are generated
// from internal/gen rather than hand-copied eight times. See README.md.
package functional

// Option carries a value that may legitimately be absent — as opposed to a
// failure. A SELECT matching zero rows is the paradigm case: not every
// "no value" is an error.
//
// It has exactly two states and holds no error. A computation that can also
// fail returns (Option[T], error), so the failure travels in the position Go
// reserves for it and errcheck can enforce that a caller looks at it. Folding
// the error in here would make it a struct field no tool inspects.
type Option[T any] struct {
	v       T
	present bool
}

// Some builds a present Option carrying v.
func Some[T any](v T) Option[T] {
	return Option[T]{v: v, present: true}
}

// None builds an absent Option: no value, no error.
func None[T any]() Option[T] {
	return Option[T]{}
}

// FromPtr lifts Go's nilable-pointer convention into an Option: a nil pointer
// becomes None, a non-nil one becomes Some of the pointed-to value. The value
// is copied out, so the Option does not alias the pointer's target — mutating
// *p afterwards does not reach into the Option.
//
// This is the bridge that matters most here: generated OpenAPI structs model
// optional fields as pointers, and FromPtr turns one into an Option at the
// boundary so the rest of the chain never has to nil-check:
//
//	name := functional.FromPtr(req.DisplayName).OrElse(defaultName)
//
// A nil pointer can only mean absence, which is exactly what Option models.
func FromPtr[T any](p *T) Option[T] {
	if p == nil {
		return None[T]()
	}
	return Some(*p)
}

// OptionOf lifts Go's comma-ok shape — a map lookup, a type assertion, a
// channel receive — straight into an Option, so the result can join a chain
// without an intermediate if:
//
//	v, ok := cache[key]
//	cached := functional.OptionOf(v, ok)
//
// Go only produces the two-value form in assignment position, so the lookup
// needs its own line first; OptionOf then absorbs the pair. ok=false is Go's
// idiom for absence, not for failure.
func OptionOf[T any](v T, ok bool) Option[T] {
	if !ok {
		return None[T]()
	}
	return Some(v)
}

// IsPresent reports whether a value is here. It answers one question only:
// a failure never reaches an Option, so absence is unambiguous.
func (o Option[T]) IsPresent() bool {
	return o.present
}

// IsAbsent reports whether no value is here — the negation of IsPresent,
// spelled out. It exists because `!found.IsPresent()` puts the negation far
// from the word it negates, and a reader scanning a chain of guards has to
// carry it across the whole expression; `found.IsAbsent()` reads as one
// thought. Both are correct, so use whichever states the guard's intent
// positively.
//
// It is IsAbsent rather than IsEmpty because this package calls the state
// absence everywhere else, and one word for one meaning is worth more than
// matching another language's spelling.
func (o Option[T]) IsAbsent() bool {
	return !o.present
}

// Value returns the value. Its result is meaningful only when IsPresent is
// true; otherwise it is T's zero value.
func (o Option[T]) Value() T {
	return o.v
}

// Map transforms the value when present, leaving absence untouched. It is a generic method (Go 1.27) so f can map to a different
// type R than the receiver's T.
func (o Option[T]) Map[R any](f func(T) R) Option[R] {
	if !o.IsPresent() {
		return Option[R]{}
	}
	return Some(f(o.v))
}

// FlatMap chains a second Option-producing computation keyed by the present
// value, letting absence from either step propagate without an intermediate
// presence check. Use this to look up A, then look up B keyed
// by A, where absence from either lookup should end the chain the same way.
func (o Option[T]) FlatMap[R any](f func(T) Option[R]) Option[R] {
	if !o.IsPresent() {
		return Option[R]{}
	}
	return f(o.v)
}

// Filter turns a present value that fails pred into absence. An already
// absent Option is unchanged.
func (o Option[T]) Filter(pred func(T) bool) Option[T] {
	if !o.IsPresent() || pred(o.v) {
		return o
	}
	return Option[T]{}
}

// OrElse returns the value if present, otherwise fallback.
func (o Option[T]) OrElse(fallback T) T {
	if o.IsPresent() {
		return o.v
	}
	return fallback
}

// OrElseGet is OrElse with the fallback computed only when it is needed, for
// a default that costs something to produce.
func (o Option[T]) OrElseGet(fallback func() T) T {
	if o.IsPresent() {
		return o.v
	}
	return fallback()
}

// Get returns the value and whether it was present, in Go's comma-ok shape.
// It is the idiomatic way out of an Option for code that is done chaining:
//
//	if v, ok := opt.Get(); ok {
//		use(v)
//	}
func (o Option[T]) Get() (T, bool) {
	return o.v, o.IsPresent()
}

// ToResult converts absence into an error. Pass the error that absence means
// *here* — a repository
// returning "no such row" as an Option leaves it to the service layer to decide
// whether that is a domain NotFound or something benign:
//
//	cfg, err := repo.FindConfig(ctx, id).ToResult(ErrConfigNotFound).Value()
func (o Option[T]) ToResult(absent error) Result[T] {
	if !o.present {
		return Failure[T](absent)
	}
	return Ok(o.v)
}

// Or substitutes an alternative Option when this one is absent — a second
// lookup to try, a computed default. Unlike OrElse it stays in Option shape,
// so the alternative may itself be absent and the chain continues either way.
// f runs only on absence, so an expensive fallback costs nothing when the
// first lookup hit.
func (o Option[T]) Or(f func() Option[T]) Option[T] {
	if o.present {
		return o
	}
	return f()
}

// Peek runs a side effect on a present value and returns the receiver
// unchanged, so a chain can be observed — logged, counted, traced — without
// being broken apart into statements. Absence skips f and flows through as it
// was; Peek observes values, it does not observe their lack.
func (o Option[T]) Peek(f func(T)) Option[T] {
	if o.IsPresent() {
		f(o.v)
	}
	return o
}

// ForEach runs f on the value when present and does nothing otherwise — the
// terminal counterpart to Peek, for when the side effect is the point and
// nothing further chains. Absence skips f.
func (o Option[T]) ForEach(f func(T)) {
	if o.IsPresent() {
		f(o.v)
	}
}

// Fold collapses the Option into a single value of another type: onPresent
// for a present value, onEmpty otherwise. It is a generic method (Go 1.27)
// so R need not be T.
//
// Both arms are total: an Option is present or it is not, so Fold always has
// exactly one answer to give.
//
//	label := opt.Fold(
//		func() string { return "unnamed" },
//		func(u User) string { return u.Name },
//	)
func (o Option[T]) Fold[R any](onEmpty func() R, onPresent func(T) R) R {
	if o.IsPresent() {
		return onPresent(o.v)
	}
	return onEmpty()
}

// Exists reports whether a value is present and satisfies pred — Filter
// collapsed to a bool, for a caller that only needs the verdict. Absence is
// false and pred never runs on it.
func (o Option[T]) Exists(pred func(T) bool) bool {
	return o.IsPresent() && pred(o.v)
}

// ToPtr converts back to Go's nilable-pointer convention — the inverse of
// FromPtr, for handing a value to code (a generated OpenAPI struct field,
// usually) that models optionality as *T. Present becomes a pointer to a
// copy, so the caller cannot mutate the Option's interior through it, and
// absent becomes nil.
func (o Option[T]) ToPtr() *T {
	if !o.IsPresent() {
		return nil
	}
	v := o.v
	return &v
}

// Contains reports whether a present value equals v. An absent Option is
// false — it does not contain anything, not even T's zero value.
//
// This is a package-level function rather than a method because Go does not
// allow a method to add constraints beyond the ones on its receiver's type
// parameters: Option[T] declares T as `any`, and a method cannot narrow that
// to `comparable` for just itself (methods introduce no new constraints on
// existing type parameters, and a generic method's own parameters cannot
// constrain the receiver's). The == below needs comparable, so the constraint
// has to live on a standalone function.
func Contains[T comparable](o Option[T], v T) bool {
	return o.IsPresent() && o.v == v
}
