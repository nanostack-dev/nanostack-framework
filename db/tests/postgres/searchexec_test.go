// This file is Nanostack-only (not in go-jet): the db/searchexec suite on PostgreSQL and
// CockroachDB. See db/UPSTREAM.md and db/tests/internal/searchexeccontract.

package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/searchexeccontract"
)

func TestSearchexecContract(t *testing.T) {
	flavor := searchexeccontract.Postgres
	if sourceIsCockroachDB() {
		flavor = searchexeccontract.CockroachDB
	}
	searchexeccontract.Run(t, searchexeccontract.Config{
		Flavor: flavor,
		DB:     db,
		Schema: "test_sample",
		NewTable: func(schema, name string, columns ...postgres.Column) keyset.Source {
			return postgres.NewTable(schema, name, "", columns...)
		},
		InnerJoin: func(left, right keyset.Source, on postgres.BoolExpression) keyset.Source {
			l, lok := left.(postgres.Table)
			r, rok := right.(postgres.Table)
			require.True(t, lok && rok)
			return l.INNER_JOIN(r, on)
		},
	})
}
