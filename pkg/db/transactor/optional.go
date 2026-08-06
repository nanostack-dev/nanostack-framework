package transactor

// Optional carries the outcome of a query that may legitimately find no
// matching row — a SELECT that turns up nothing, or an UPDATE/DELETE ...
// RETURNING whose WHERE clause matches nothing.
//
// It exists as an alternative to QueryOptional's (*T, error) shape for
// callers who don't want "nil pointer" doing double duty as the signal for
// absence. IsPresent answers "was there a row" and Err answers "did anything
// actually go wrong" — two separate questions that a nilable pointer folds
// into one, and that a redundant isError bool next to err would only restate.
// Value returns the row itself, so the caller checking IsPresent never
// touches a pointer at all:
//
//	result := repo.UpdateOptional(ctx, tenantID, instance)
//	if err := result.Err(); err != nil {
//		return err // a real failure
//	}
//	if !result.IsPresent() {
//		return nil // the row is gone — benign, nothing to do
//	}
//	updated := result.Value()
//
// Optional has no On* translation methods the way Result does. Absence here
// is not a driver error to translate — it is success carrying no row — so
// there is nothing for OnUnique/OnForeignKey/OnNoRows to match against.
type Optional[T any] struct {
	v       T
	present bool
	err     error
}

// IsPresent reports whether the query found a row. It is false both when the
// query matched nothing (Err is nil) and when the query failed (Err is
// non-nil) — check Err first to tell those apart.
func (o Optional[T]) IsPresent() bool {
	return o.err == nil && o.present
}

// Err returns the error, if the query failed. Finding no row is not a
// failure and does not set this.
func (o Optional[T]) Err() error {
	return o.err
}

// Value returns the row. Its result is meaningful only when IsPresent is
// true; otherwise it is T's zero value.
func (o Optional[T]) Value() T {
	return o.v
}
