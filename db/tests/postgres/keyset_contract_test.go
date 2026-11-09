// This file is Nanostack-only (not in go-jet): the db/keyset contract test on
// PostgreSQL. See db/UPSTREAM.md and db/tests/internal/keysetcontract.

package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/keysetcontract"
)

func TestKeysetContract(t *testing.T) {
	// ADR 0015 drops CockroachDB, which has no enum order.
	skipForCockroachDB(t)
	keysetcontract.Run(t, keysetcontract.Config{
		Flavor: keysetcontract.Postgres,
		DB:     db,
		Schema: "public",
		NewTable: func(schema, name string, columns ...postgres.Column) keyset.Source {
			return postgres.NewTable(schema, name, "", columns...)
		},
		Text: postgres.String,
		Subquery: func(
			table keyset.Source, projection postgres.Projection, where postgres.BoolExpression,
		) postgres.Expression {
			children, ok := table.(postgres.Table)
			require.True(t, ok)
			return children.SELECT(projection).WHERE(where)
		},
	})
}
