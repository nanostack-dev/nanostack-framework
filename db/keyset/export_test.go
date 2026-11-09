package keyset

import (
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// Test-only hooks, so the external test package can assert the SQL of a page
// without a database. They resolve the source as Run does and panic when it
// is not a PostgreSQL table, which a test never passes.

func (b *PageBuilder[T, R]) CountStatementForTest() jet.Statement {
	return b.countStatement(b.mustTable())
}

func (b *PageBuilder[T, R]) PageStatementForTest(page search.Pagination) jet.Statement {
	return b.pageStatement(b.mustTable(), page)
}

func (b *PageBuilder[T, R]) KeysetStatementForTest(page search.Pagination, seek jet.BoolExpression) jet.Statement {
	return b.keysetStatement(b.mustTable(), page, seek)
}

func (b *PageBuilder[T, R]) mustTable() jet.ReadableTable {
	table, err := readable(b.source)
	if err != nil {
		panic(err)
	}
	return table
}
