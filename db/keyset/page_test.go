package keyset_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

type sortField string

const (
	sortByName    sortField = "NAME"
	sortByCreated sortField = "CREATED_AT"
	sortUnmapped  sortField = "NOT_A_COLUMN"
)

type row struct{ Name string }

type domain struct{ Name string }

func toDomain(r row) domain { return domain(r) }

var (
	colName    = jet.StringColumn("name")
	colTenant  = jet.StringColumn("tenant_id")
	colCreated = jet.TimestampzColumn("created_at")
	testTable  = jet.NewTable("public", "widgets", "", colName, colTenant, colCreated)
)

func builder() *keyset.PageBuilder[row, domain] {
	return keyset.Page(nil, toDomain, colName, colTenant).From(testTable)
}

func normalize(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

func TestPageCountStatement(t *testing.T) {
	t.Run("counts rows, not columns, and ignores ordering", func(t *testing.T) {
		sql := normalize(builder().OrderBy(colName.ASC()).CountStatementForTest().DebugSql())
		require.Contains(t, sql, "SELECT COUNT(*)")
		require.NotContains(t, sql, "ORDER BY", "the count only needs the total")
		require.NotContains(t, sql, "LIMIT", "Total covers every match")
	})

	t.Run("applies the same filter as the page", func(t *testing.T) {
		where := colTenant.EQ(jet.String("tenant1"))
		count := normalize(builder().Where(where).CountStatementForTest().DebugSql())
		page := normalize(builder().Where(where).PageStatementForTest(search.Pagination{Limit: 10}).DebugSql())
		for _, sql := range []string{count, page} {
			require.Contains(t, sql, "widgets.tenant_id = 'tenant1'")
		}
	})

	t.Run("a nil filter selects everything", func(t *testing.T) {
		require.NotContains(t, normalize(builder().CountStatementForTest().DebugSql()), "WHERE")
	})
}

func TestPagePageStatement(t *testing.T) {
	t.Run("selects the projected columns", func(t *testing.T) {
		sql := normalize(builder().PageStatementForTest(search.Pagination{Limit: 10}).DebugSql())
		require.Contains(t, sql, "widgets.name")
		require.Contains(t, sql, "widgets.tenant_id")
	})

	t.Run("applies limit and offset", func(t *testing.T) {
		sql := normalize(builder().PageStatementForTest(search.Pagination{Limit: 25, Offset: 50}).DebugSql())
		require.Contains(t, sql, "LIMIT 25")
		require.Contains(t, sql, "OFFSET 50")
	})

	t.Run("orders by every clause in the order given", func(t *testing.T) {
		sql := normalize(builder().
			OrderBy(colCreated.DESC(), colName.ASC()).
			PageStatementForTest(search.Pagination{Limit: 10}).DebugSql())
		require.Contains(t, sql, "ORDER BY widgets.created_at DESC, widgets.name ASC")
	})
}

func keysetBuilder() *keyset.PageBuilder[keyRow, keyRow] {
	return keyset.Page(nil, func(r keyRow) keyRow { return r }, colID, colName).From(testTable)
}

func TestKeysetStatement(t *testing.T) {
	order := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}},
		keyColumns(),
		idKey(),
	)
	where := colTenant.EQ(jet.String("t1"))
	seek, err := order.Seek(cursorFor(t, order, keyRow{ID: "w_1", Name: "a"}))
	require.NoError(t, err)

	sql := normalize(keysetBuilder().Where(where).Keyset(order).
		KeysetStatementForTest(search.Pagination{Limit: 10}, seek).DebugSql())
	for _, want := range []string{
		"widgets.tenant_id = 't1'",
		"ROW(widgets.name, widgets.id) > ROW('a'::text, 'w_1'::text)",
		"ORDER BY widgets.name ASC, widgets.id ASC",
		"LIMIT 11",
	} {
		require.Contains(t, sql, want)
	}
	require.NotContains(t, sql, "OFFSET", "a cursor page has no offset")

	sql = normalize(keysetBuilder().Keyset(order).
		KeysetStatementForTest(search.Pagination{Limit: 10, Offset: 30}, nil).DebugSql())
	require.Contains(t, sql, "LIMIT 11")
	require.Contains(t, sql, "OFFSET 30")
}

func TestRunRefusesABadPageBeforeQuerying(t *testing.T) {
	order := mustOrder[sortField](t, nil, keyColumns(), idKey())
	byCreated := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortAscending}}, keyColumns(), idKey())
	mysqlTable := mysql.NewTable("db", "widgets", "", mysql.StringColumn("id"))
	sqliteTable := sqlite.NewTable("", "widgets", "", sqlite.StringColumn("id"))
	for name, tc := range map[string]struct {
		builder *keyset.PageBuilder[keyRow, keyRow]
		page    search.Pagination
		want    error
	}{
		"no from": {
			keyset.Page(nil, func(r keyRow) keyRow { return r }, colID), search.Pagination{Limit: 1},
			keyset.ErrPageSourceMissing,
		},
		"a timestamptz key on a MySQL table": {
			keysetBuilder().From(mysqlTable).Keyset(byCreated), search.Pagination{Limit: 1}, keyset.ErrDialect,
		},
		"a timestamptz key on a SQLite table": {
			keysetBuilder().From(sqliteTable).Keyset(byCreated), search.Pagination{Limit: 1}, keyset.ErrDialect,
		},
		"not a table": {keysetBuilder().From("widgets"), search.Pagination{Limit: 1}, keyset.ErrDialect},
		"cursor with offset": {
			keysetBuilder().Keyset(order), search.Pagination{Limit: 1, Offset: 1, Cursor: "x"},
			search.ErrCursorWithOffset,
		},
		"cursor without keyset": {keysetBuilder(), search.Pagination{Limit: 1, Cursor: "x"}, search.ErrInvalidCursor},
		"undecodable cursor": {
			keysetBuilder().Keyset(order), search.Pagination{Limit: 1, Cursor: "x"}, search.ErrInvalidCursor,
		},
		"keyset and order by": {
			keysetBuilder().Keyset(order).OrderBy(colName.ASC()), search.Pagination{Limit: 1},
			keyset.ErrPageOrderConflict,
		},
		"required where is nil": {
			keysetBuilder().Keyset(order).RequireWhere(), search.Pagination{Limit: 1}, search.ErrUnscoped,
		},
		"required where is set to nil": {
			keysetBuilder().RequireWhere().Where(nil), search.Pagination{Limit: 1}, search.ErrUnscoped,
		},
	} {
		t.Run(name, func(t *testing.T) {
			// db is nil: reaching a query would panic, so an error proves the
			// page was refused up front.
			_, err := tc.builder.Run(t.Context(), tc.page)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// RequireWhere is search mode: an unscoped search is a programming error, so
// it must never be a client error.
func TestRequireWhereIsNotAClientError(t *testing.T) {
	_, err := keysetBuilder().RequireWhere().Run(t.Context(), search.Pagination{Limit: 1})
	require.ErrorIs(t, err, search.ErrUnscoped)
	require.False(t, search.IsClientError(err))
}
