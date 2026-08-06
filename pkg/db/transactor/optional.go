package transactor

import "github.com/nanostack-dev/nanostack-framework/pkg/functional"

// Optional carries the outcome of a query that may legitimately find no
// matching row — a SELECT that turns up nothing, or an UPDATE/DELETE ...
// RETURNING whose WHERE clause matches nothing.
//
// It exists as an alternative to a (*T, error) shape for callers who don't
// want "nil pointer" doing double duty as the signal for absence. IsPresent
// answers "was there a row" and Err answers "did anything actually go
// wrong" — two separate questions that a nilable pointer folds into one:
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
// Optional is a type alias for functional.Option[T] — the SQL layer has no
// translation rules of its own to add (see Result, which does), so there is
// nothing here beyond the alias, and Map/FlatMap/Filter/OrElse come from
// functional as-is. Chain a second Optional-producing lookup keyed by this
// one's value with FlatMap:
//
//	config := transactor.QueryOptional[Run](ctx, db, runStmt).
//		FlatMap(func(run Run) transactor.Optional[Config] {
//			return transactor.QueryOptional[Config](ctx, db, configStmtFor(run))
//		})
type Optional[T any] = functional.Option[T]
