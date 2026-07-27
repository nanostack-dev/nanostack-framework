// Package pgerr classifies PostgreSQL driver errors by SQLSTATE so service code
// can map them to domain errors without importing the driver or matching on
// message text.
//
// It exists because a uniqueness pre-check is never race-free: a concurrent
// inserter's row stays invisible until it commits, so two callers can both pass
// their check and the loser trips the unique index. The constraint violation is
// the only race-free answer, and translating it needs driver-specific
// unwrapping that each service was otherwise hand-rolling.
package pgerr

import (
	"errors"

	"github.com/lib/pq"
)

// UniqueViolation is the SQLSTATE raised when an insert or update violates a
// unique constraint or unique index.
const UniqueViolation = "23505"

// IsUniqueViolation reports whether err is a unique-constraint violation
// (SQLSTATE 23505).
//
// When one or more constraints are given, the violation must name one of them;
// with none, any unique violation matches. Postgres reports the constraint or
// unique index name, so pass the index name for uniqueness declared as
// CREATE UNIQUE INDEX rather than as a table constraint.
//
// The error is unwrapped with errors.As, so it is matched through go-jet's
// "jet: " wrapping and any fmt.Errorf("%w") chain.
//
// Constraint names are schema knowledge and stay with the caller:
//
//	if pgerr.IsUniqueViolation(err, "idx_platform_tenant_email") {
//		return ErrInvitationAlreadyExists
//	}
func IsUniqueViolation(err error, constraints ...string) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != UniqueViolation {
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
