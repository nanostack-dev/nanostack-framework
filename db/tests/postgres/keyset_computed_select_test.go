// This file is Nanostack-only (not in go-jet): the computed keys of db/keyset
// against PostgreSQL, when their expression is NULL or their projection is
// already selected. See db/UPSTREAM.md.

package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// A NOT NULL computed key over a MAX that is NULL for a widget with no
// request reads that NULL as 0 in the order, the seek and the scan, so the
// walk returns every widget.
func TestIntegrationKeysetComputedNotNullKeyOverANullableMaxIsComplete(t *testing.T) {
	conn := keysetDB(t)
	setupComputedKeysetData(t, conn, 31, 60)
	counts, _ := requestStats(t, conn)

	for _, direction := range []search.SortDirection{search.SortAscending, search.SortDescending} {
		order := ksComputedOrder(t, search.Sort[kcField]{Field: ksBiggestOr0, Direction: direction})
		for _, limit := range []int32{1, 4} {
			got := ksComputedWalk(t, conn, order, limit)
			require.Len(t, got, len(counts), "%s limit %d: every widget", order.Signature(), limit)
			seen := map[string]bool{}
			zeros := 0
			for _, row := range got {
				require.False(t, seen[row.ID], "row %s returned twice", row.ID)
				seen[row.ID] = true
				if counts[row.ID] == 0 {
					require.Zero(t, row.BiggestOr0, "a widget with no request reads 0")
					zeros++
				}
			}
			require.Positive(t, zeros, "some widgets have no request")
		}
	}
}

// A page whose columns select a computed key with Key.Projection, and whose
// order uses that key, selects it once: the scan runs with StrictScan, which
// refuses a repeated alias. The field is filled on the other orders too.
func TestIntegrationKeysetComputedProjectionInTheColumnsIsSelectedOnce(t *testing.T) {
	conn := keysetDB(t)
	setupComputedKeysetData(t, conn, 41, 40)
	counts, _ := requestStats(t, conn)
	requests := ksComputedKeys()[ksRequests]
	columns := []jet.Projection{ksID, ksNameC, requests.Projection()}

	for _, field := range []kcField{ksRequests, kcName} {
		order := ksComputedOrder(t, search.Sort[kcField]{Field: field, Direction: search.SortDescending})
		page := search.Pagination{Limit: 7}
		var got []ksComputedRow
		for range 100 {
			result, err := keyset.Page(conn, func(r ksComputedRow) ksComputedRow { return r }, columns...).
				From(ksTable).
				Where(ksTenantC.EQ(jet.String("t1"))).
				Keyset(order).
				Run(t.Context(), page)
			require.NoError(t, err)
			got = append(got, result.Items...)
			if !result.HasMore() {
				break
			}
			page.Cursor = result.NextCursor
		}
		require.Len(t, got, len(counts), "%s: every widget", order.Signature())
		for _, row := range got {
			require.Equal(t, counts[row.ID], row.Requests, "the scan reads the count on every order")
		}
	}
}
