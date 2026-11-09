// This file is Nanostack-only (not in go-jet): db/keyset on MySQL and
// MariaDB. See db/UPSTREAM.md.

package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

type ksComputedRow struct {
	ID       string `alias:"widgets.id"`
	Requests int64  `alias:"widget_row.request_count"`
}

// The computed keys write PostgreSQL SQL. Until the pager supports this
// dialect, a page with a computed key is ErrDialect, before any statement
// runs.
func TestKeysetComputedKeyIsErrDialect(t *testing.T) {
	widgetID := mysql.StringColumn("id")
	widgets := mysql.NewTable("test_sample", "widgets", "", widgetID)
	requestWidget := mysql.StringColumn("widget_id")
	requests := mysql.NewTable("test_sample", "requests", "", requestWidget)

	order, err := keyset.OrderOf(
		[]search.Sort[string]{{Field: "request_count", Direction: search.SortDescending}},
		map[string]keyset.Key[ksComputedRow]{
			"request_count": keyset.IntExprKey("widget_row.request_count",
				mysql.IntExp(requests.SELECT(mysql.COUNT(mysql.STAR)).WHERE(requestWidget.EQ(widgetID))),
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
