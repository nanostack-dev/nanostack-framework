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
	"strings"

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

const (
	// QueryCanceled is raised when Postgres aborts a statement already in
	// flight. It covers two unrelated causes — a cancel request sent by the
	// client, and the server enforcing statement_timeout — and only the message
	// text tells them apart. Match it through IsQueryCanceled rather than Is, so
	// a timeout is not read as a cancellation.
	QueryCanceled = "57014"
	// canceledByUser is the message fragment Postgres uses for the client-driven
	// half of QueryCanceled: "canceling statement due to user request".
	canceledByUser = "due to user request"
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

// IsQueryCanceled reports whether err is a statement Postgres aborted because
// the client asked it to (SQLSTATE 57014, "canceling statement due to user
// request").
//
// lib/pq sends that cancel request when the context driving a query is canceled
// — a client that hung up, or a parent deadline — and reports the abort as a
// fresh *pq.Error rather than one wrapping context.Canceled. So errors.Is
// against the context sentinels misses it even though the caller going away is
// the whole cause, and code choosing a log severity or a retry has to ask here
// instead.
//
// The match is deliberately narrower than the SQLSTATE. A statement_timeout kill
// carries the same 57014 but is a server-side fault worth an error-level log and
// an investigation, and nothing but the message separates the two.
//
// The error is unwrapped with errors.As on the same terms as Is. A nil error
// never matches, and neither does an error that merely quotes the message text
// without carrying the SQLSTATE.
func IsQueryCanceled(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != QueryCanceled {
		return false
	}
	return strings.Contains(pqErr.Message, canceledByUser)
}
