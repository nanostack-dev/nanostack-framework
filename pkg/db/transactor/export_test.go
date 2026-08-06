package transactor

import (
	jet "github.com/go-jet/jet/v2/postgres"

	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// NewResultForTest builds a Result the way the query helpers do, so the
// external test package can exercise error translation without a database.
// The testpackage linter skips export_test.go by design.
func NewResultForTest[T any](v T, err error) Result[T] {
	return newResult(v, err)
}

// NewOptionalForTest builds an Optional in each of its three states, so the
// external test package can assert IsPresent/Err/Value without a database.
// present is ignored when err is non-nil, mirroring IsPresent's own rule that
// an error always means "not present.".
func NewOptionalForTest[T any](v T, present bool, err error) Optional[T] {
	return Optional[T]{v: v, present: present, err: err}
}

// Test-only re-exports so the external test package can assert the SQL a
// PageBuilder produces without needing a database.
func (b *PageBuilder[T, R]) CountStatementForTest() jet.Statement {
	return b.countStatement()
}

func (b *PageBuilder[T, R]) PageStatementForTest(pagination search.Pagination) jet.Statement {
	return b.pageStatement(pagination)
}
