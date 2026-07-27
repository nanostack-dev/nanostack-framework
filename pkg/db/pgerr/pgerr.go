// Package pgerr classifies PostgreSQL driver errors by SQLSTATE so service code
// can map them to domain errors without importing the driver or matching on
// message text.
//
// It exists because a uniqueness pre-check is never race-free: a concurrent
// inserter's row stays invisible until it commits, so two callers can both pass
// their check and the loser trips the unique index. The constraint violation is
// the only race-free answer, and translating it needs driver-specific
// unwrapping that each service was otherwise hand-rolling.
//
// For query call sites, pkg/db/transactor exposes the same classification as
// fluent Result methods. Use this package directly for code that does not go
// through those helpers.
package pgerr

import (
	"errors"

	"github.com/lib/pq"
)

// SQLSTATE codes for the integrity-constraint violations that services map to
// client errors. See https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	// UniqueViolation is raised when an insert or update violates a unique
	// constraint or unique index. Primary keys are unique constraints, so a
	// duplicate primary key reports this code too.
	UniqueViolation = "23505"
	// ForeignKeyViolation is raised when a write references a missing parent
	// row, or when a delete is blocked by ON DELETE RESTRICT / NO ACTION.
	ForeignKeyViolation = "23503"
)

// Is reports whether err is a PostgreSQL error carrying the given SQLSTATE.
//
// When one or more constraints are given, the error must name one of them; with
// none, any error carrying the code matches. Postgres reports the constraint or
// unique index name, so pass the index name for uniqueness declared as
// CREATE UNIQUE INDEX rather than as a table constraint.
//
// The error is unwrapped with errors.As, so it is matched through go-jet's
// "jet: " wrapping and any fmt.Errorf("%w") chain. A nil error never matches.
func Is(err error, code string, constraints ...string) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != code {
		return false
	}
	if len(constraints) == 0 {
		return true
	}
	for _, constraint := range constraints {
		if pqErr.Constraint == constraint {
			return true
		}
	}
	return false
}

// IsUniqueViolation reports whether err is a unique-constraint violation
// (SQLSTATE 23505), optionally narrowed to one of the named constraints.
//
// Passing no constraint matches any unique violation, including a primary-key
// collision. That is rarely what a caller means: a duplicate generated ID is a
// bug, not a duplicate business key, and a unique index added by a later
// migration would silently widen the match. Name the constraint unless the
// table genuinely has only one way to collide.
//
// Constraint names are schema knowledge and stay with the caller:
//
//	if pgerr.IsUniqueViolation(err, "idx_platform_tenant_email") {
//		return ErrInvitationAlreadyExists
//	}
func IsUniqueViolation(err error, constraints ...string) bool {
	return Is(err, UniqueViolation, constraints...)
}

// IsForeignKeyViolation reports whether err is a foreign-key violation
// (SQLSTATE 23503), optionally narrowed to one of the named constraints.
//
// This covers both directions: writing a row whose parent does not exist, and
// deleting a row that ON DELETE RESTRICT still protects. Both are caller
// mistakes rather than server faults, so they belong on a 4xx path.
func IsForeignKeyViolation(err error, constraints ...string) bool {
	return Is(err, ForeignKeyViolation, constraints...)
}
