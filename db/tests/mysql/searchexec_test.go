// This file is Nanostack-only (not in go-jet): the db/searchexec core on MySQL and MariaDB. See
// db/UPSTREAM.md.

package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	. "github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

type seRow struct {
	ID  string `alias:"items.id"`
	Org string `alias:"items.org"`
}

type seSearch struct {
	searchexec.Query[seRow, seSearch, string, string]
}

// TestSearchexecRefusesThisDialect pins that the search runtime refuses a source of this
// dialect with keyset.ErrDialect, before any statement runs: it supports PostgreSQL only
// until the PR that adds MySQL, MariaDB and SQLite (R10) replaces this test with the
// PostgreSQL suite.
func TestSearchexecRefusesThisDialect(t *testing.T) {
	id, org := StringColumn("id"), StringColumn("org")
	items := NewTable("test_sample", "items", "", id, org)
	spec := searchexec.DefineSpec(searchexec.SpecDef[seRow, string, string]{
		From:     func(string) keyset.Source { return items },
		Columns:  ProjectionList{id, org},
		Scope:    []searchexec.ScopeCol[string]{searchexec.ScopeEq(searchexec.Value[string], org)},
		Keys:     map[string]keyset.Key[seRow]{"org": keyset.StringKey(org, func(r seRow) string { return r.Org })},
		Tiebreak: []keyset.Key[seRow]{keyset.StringKey(id, func(r seRow) string { return r.ID })},
		Limit:    10,
	})
	q := searchexec.New[seSearch](spec, "org-a")

	_, err := searchexec.Resolve(t.Context(), spec, q.Query)
	require.ErrorIs(t, err, keyset.ErrDialect)
	_, err = searchexec.Run(t.Context(), db, spec, q.Query, func(r seRow) seRow { return r })
	require.ErrorIs(t, err, keyset.ErrDialect)
	require.False(t, search.IsClientError(err))
}
