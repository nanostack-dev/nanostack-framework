package transactor

import (
	"context"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// NewResultForTest builds a Result the way the query helpers do, so the
// external test package can exercise error translation without a database.
// The testpackage linter skips export_test.go by design.
func NewResultForTest[T any](v T, err error) Result[T] {
	return newResult(v, err)
}

// WithRequestedRowLockForTest exposes the clause a locked context adds, so the
// external test package can assert the SQL without a database.
func WithRequestedRowLockForTest(ctx context.Context, stmt jet.Statement) (jet.Statement, error) {
	return withRequestedRowLock(ctx, stmt)
}
