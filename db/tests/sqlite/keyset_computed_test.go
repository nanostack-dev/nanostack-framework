// This file is Nanostack-only (not in go-jet): db/keyset on SQLite. See
// db/UPSTREAM.md.

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

type ksComputedRow struct {
	ID       string `alias:"widgets.id"`
	Requests int64  `alias:"widget_row.request_count"`
}

// The computed keys write PostgreSQL SQL. Until the pager supports this
// dialect, a page with a computed key is ErrDialect, before any statement
// runs.
func TestKeysetComputedKeyIsErrDialect(t *testing.T) {
	widgetID := sqlite.StringColumn("id")
	widgets := sqlite.NewTable("", "widgets", "", widgetID)
	requestWidget := sqlite.StringColumn("widget_id")
	requests := sqlite.NewTable("", "requests", "", requestWidget)

	order, err := keyset.OrderOf(
		[]search.Sort[string]{{Field: "request_count", Direction: search.SortDescending}},
		map[string]keyset.Key[ksComputedRow]{
			"request_count": keyset.IntExprKey("widget_row.request_count",
				sqlite.IntExp(requests.SELECT(sqlite.COUNT(sqlite.STAR)).WHERE(requestWidget.EQ(widgetID))),
				func(r ksComputedRow) int64 { return r.Requests }),
		},
		keyset.StringKey(widgetID, func(r ksComputedRow) string { return r.ID }),
	)
	require.NoError(t, err)

	_, err = keyset.Page(db, func(r ksComputedRow) ksComputedRow { return r }, widgetID).
		From(widgets).
		Keyset(order).
		Run(t.Context(), search.Pagination{Limit: 10})
	require.ErrorIs(t, err, keyset.ErrDialect)
}
