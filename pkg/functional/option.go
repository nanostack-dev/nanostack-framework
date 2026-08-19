// Package functional provides small, generic value types for chaining
// computations that may be absent or may fail — Option[T] (a value that may
// legitimately be absent), Result[T] (a value that may have failed), and
// Tuple2..Tuple9 (fixed-arity value grouping, for a step that needs to pass
// more than one value to the next).
//
// The distinction the package exists for is that absent is not failed: a
// SELECT matching zero rows is success carrying no row. Every method here
// preserves that ordering — a failure outranks absence and is never quietly
// turned into it — so a caller that checks Err before IsPresent can always
// tell an outage from a missing value.
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
// A nil pointer can only mean absence — a *T carries no error channel — so
// FromPtr never produces the failed state.
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
// needs its own line first; OptionOf then absorbs the pair. Like FromPtr it
// never produces the failed state — ok=false is Go's idiom for absence, not
// for failure.
func OptionOf[T any](v T, ok bool) Option[T] {
	if !ok {
		return None[T]()
	}
	return Some(v)
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
//
// Like IsPresent it reports false for a failure as well as for absence, so a
// caller that must tell those apart checks Err first.
func (o Option[T]) Get() (T, bool) {
	return o.v, o.IsPresent()
}

// ToResult converts absence into an error, collapsing an Option's three states
// into a Result's two. Pass the error that absence means *here* — a repository
// returning "no such row" as an Option leaves it to the service layer to decide
// whether that is a domain NotFound or something benign:
//
//	cfg, err := repo.FindConfig(ctx, id).ToResult(ErrConfigNotFound).Value()
//
// An existing failure outranks absent: a real error is never replaced by the
// caller's not-found sentinel.
func (o Option[T]) ToResult(absent error) Result[T] {
	if o.err != nil {
		return Failure[T](o.err)
	}
	if !o.present {
		return Failure[T](absent)
	}
	return Ok(o.v)
}

// Or substitutes an alternative Option when this one is legitimately absent —
// a second lookup to try, a computed default. Unlike OrElse it stays in
// Option shape, so the alternative may itself be absent or failed and the
// chain continues either way. f runs only on absence, so an expensive
// fallback lookup costs nothing when the first one hit.
//
// A failure passes through untouched and f is never called: trying the
// fallback after an outage would report whatever the fallback finds in place
// of the error, turning a broken primary into a quietly wrong answer.
func (o Option[T]) Or(f func() Option[T]) Option[T] {
	if o.present || o.err != nil {
		return o
	}
	return f()
}

// Peek runs a side effect on a present value and returns the receiver
// unchanged, so a chain can be observed — logged, counted, traced — without
// being broken apart into statements. Absence and failure skip f and flow
// through as they were; Peek observes values, it does not observe their lack.
func (o Option[T]) Peek(f func(T)) Option[T] {
	if o.IsPresent() {
		f(o.v)
	}
	return o
}

// ForEach runs f on the value when present and does nothing otherwise — the
// terminal counterpart to Peek, for when the side effect is the point and
// nothing further chains. Absence and failure both skip f; a caller that must
// react to a failure checks Err first.
func (o Option[T]) ForEach(f func(T)) {
	if o.IsPresent() {
		f(o.v)
	}
}

// Fold collapses the Option into a single value of another type: onPresent
// for a present value, onEmpty otherwise. It is a generic method (Go 1.27)
// so R need not be T.
//
// A FAILED Option folds through onEmpty. That is deliberate, and it is the
// same contract OrElse and Get already have: R carries no error channel, so a
// two-arm fold has nowhere for the error to survive, and inventing a third
// arm would make Fold a worse ToResult. The failure is not lost — it is still
// on the receiver — but Fold itself cannot report it, so a caller whose empty
// arm must react differently to an outage than to a missing value checks Err
// before folding, or folds the Result instead:
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
// collapsed to a bool, for a caller that only needs the verdict. Absence and
// failure are both false, and pred never runs on either; like IsPresent this
// cannot distinguish the two, so check Err first when that matters.
func (o Option[T]) Exists(pred func(T) bool) bool {
	return o.IsPresent() && pred(o.v)
}

// ToPtr converts back to Go's nilable-pointer convention — the inverse of
// FromPtr, for handing a value to code (a generated OpenAPI struct field,
// usually) that models optionality as *T. Present becomes a pointer to a
// copy, so the caller cannot mutate the Option's interior through it; absent
// AND failed both become nil, because a *T has no error channel to carry the
// failure — check Err before converting when a failure must not flatten into
// a nil field.
func (o Option[T]) ToPtr() *T {
	if !o.IsPresent() {
		return nil
	}
	v := o.v
	return &v
}

// Contains reports whether a present value equals v. Absence and failure are
// both false — a failed lookup does not contain anything, not even T's zero
// value, so a Failed Option never matches the zero value by accident.
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
