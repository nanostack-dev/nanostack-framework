package keyset

import (
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// Test-only hooks, so the external test package can assert the SQL of a page
// without a database. They resolve the source as Run does and panic when it
// is not a table, which a test never passes.

func (b *PageBuilder[T, R]) CountStatementForTest() jet.Statement {
	return b.mustSource().count(b.where)
}

func (b *PageBuilder[T, R]) PageStatementForTest(page search.Pagination) jet.Statement {
	return b.pageStatement(b.mustSource(), page)
}

func (b *PageBuilder[T, R]) KeysetStatementForTest(page search.Pagination, seek jet.BoolExpression) jet.Statement {
	computed, err := b.computedColumns()
	if err != nil {
		panic(err)
	}
	return b.keysetStatement(b.mustSource(), page, seek, computed)
}

func (b *PageBuilder[T, R]) mustSource() pageSource {
	source, err := resolveSource(b.source)
	if err != nil {
		panic(err)
	}
	return source
}

// StatementsForTest resolves the page as Run does and returns the count and
// row statements Run would send, without a database.
func (b *PageBuilder[T, R]) StatementsForTest(page search.Pagination) (jet.Statement, jet.Statement, error) {
	source, seek, err := b.prepare(page)
	if err != nil {
		return nil, nil, err
	}
	if b.keyset == nil {
		return source.count(b.where), b.pageStatement(source, page), nil
	}
	computed, err := b.computedColumns()
	if err != nil {
		return nil, nil, err
	}
	return source.count(b.where), b.keysetStatement(source, page, seek, computed), nil
}
