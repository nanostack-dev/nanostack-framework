package transactor

import (
	"context"

	jet "github.com/go-jet/jet/v2/postgres"

	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// NewResultForTest builds a Result the way the query helpers do, so the
// external test package can exercise error translation without a database.
// The testpackage linter skips export_test.go by design.
func NewResultForTest[T any](v T, err error) Result[T] {
	return newResult(v, err)
}

// Test-only re-exports so the external test package can assert the SQL a
// PageBuilder produces without needing a database.
func (b *PageBuilder[T, R]) CountStatementForTest() jet.Statement {
	return b.countStatement()
}

func (b *PageBuilder[T, R]) PageStatementForTest(pagination search.Pagination) jet.Statement {
	return b.pageStatement(pagination)
}

// WithRequestedRowLockForTest exposes the clause a locked context adds, so the
// external test package can assert the SQL without a database.
func WithRequestedRowLockForTest(ctx context.Context, stmt jet.Statement) (jet.Statement, error) {
	return withRequestedRowLock(ctx, stmt)
}
