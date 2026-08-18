package transactor

import (
	"github.com/nanostack-dev/nanostack-framework/pkg/db/pgerr"
)

// Result carries the outcome of a statement so driver errors can be translated
// into domain errors before the caller unwraps them.
//
// Every query helper returns a Result. Callers that need no translation call
// Value (or Err, for statements with no value) immediately:
//
//	inv, err := transactor.QueryMap(ctx, db, stmt, mapper.ToDomain).Value()
//
// Callers that do chain On* methods, each of which replaces the error when it
// matches:
//
//	inv, err := transactor.QueryMap(ctx, db, stmt, mapper.ToDomain).
//		OnUnique(ErrInvitationExists, "idx_platform_tenant_email").
//		Value()
//
// The first matching rule wins: once a rule replaces the driver error with a
// domain error, later rules no longer see a *pq.Error and cannot match. A nil
// error matches nothing, so On* calls are no-ops on success.
//
// Translation targets belong to the layer that owns the rule. Repositories
// should map to repository or domain sentinels, not to HTTP faults — keeping
// transport concerns out of the data layer.
type Result[T any] struct {
	v   T
	err error
}

// newResult pairs a value with its error. Helpers construct results this way so
// the zero value of T is preserved on failure.
func newResult[T any](v T, err error) Result[T] {
	return Result[T]{v: v, err: err}
}

// Value returns the value and the error, after any translation.
func (r Result[T]) Value() (T, error) {
	return r.v, r.err
}

// Err returns only the error, for statements whose value carries no
// information — Exec, and queries whose result the caller discards.
func (r Result[T]) Err() error {
	return r.err
}

// OnSQLState replaces the error with target when it is a PostgreSQL error
// carrying the given SQLSTATE, optionally narrowed to one of the named
// constraints. It is the general form behind OnUnique and OnForeignKey, and the
// escape hatch for codes this package does not name.
func (r Result[T]) OnSQLState(code string, target error, constraints ...string) Result[T] {
	if pgerr.Is(r.err, code, constraints...) {
		return Result[T]{v: r.v, err: target}
	}
	return r
}

// OnUnique replaces the error with target when the statement violated a unique
// constraint (SQLSTATE 23505), optionally narrowed to one of the named
// constraints.
//
// Naming the constraint is strongly preferred. Primary keys are unique
// constraints, so an un-narrowed rule also swallows a duplicate generated ID —
// a bug reported to the caller as a benign conflict — and a unique index added
// by a later migration widens the match with nothing in CI to catch it.
//
// Postgres reports the constraint or unique index name, so pass the index name
// for uniqueness declared with CREATE UNIQUE INDEX.
func (r Result[T]) OnUnique(target error, constraints ...string) Result[T] {
	return r.OnSQLState(pgerr.UniqueViolation, target, constraints...)
}

// OnForeignKey replaces the error with target when the statement violated a
// foreign key (SQLSTATE 23503), optionally narrowed to one of the named
// constraints.
//
// This covers a write naming a parent row that does not exist and a delete that
// ON DELETE RESTRICT still protects. Both are caller mistakes, so they belong
// on a 4xx path rather than surfacing as an unhandled server error.
func (r Result[T]) OnForeignKey(target error, constraints ...string) Result[T] {
	return r.OnSQLState(pgerr.ForeignKeyViolation, target, constraints...)
}

// OnNoRows replaces the error with target when the statement matched no rows —
// sql.ErrNoRows (database/sql) or qrm.ErrNoRows (go-jet), the two forms an
// UPDATE/DELETE ... RETURNING reports when its WHERE clause matches nothing.
//
// Unlike OnUnique and OnForeignKey this has no SQLSTATE behind it — the row is
// simply not there — so the caller must judge whether that is benign here (a
// concurrent delete beat the statement to it) or a bug (updating an ID that
// should exist). Naming target keeps that judgment at the call site instead of
// baking a blanket answer into transactor.
func (r Result[T]) OnNoRows(target error) Result[T] {
	if isNoRows(r.err) {
		return Result[T]{v: r.v, err: target}
	}
	return r
}
