//go:build integration

package repotest

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
)

// These tests run the generated builder of search Item (search.go) on
// PostgreSQL: each field selects its rows of the tenant and no row of
// another tenant, and a walk of one row per page on each sort returns every
// row of the tenant once, in the order PostgreSQL gives.

// run runs one page of q.
func run(t *testing.T, db *sql.DB, q ItemSearch) search.Result[Item] {
	t.Helper()
	res, err := searchexec.Run(context.Background(), db, itemSearch, q.Query, itemToDomain)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

// Each generated field selects the rows its condition names, of the
// tenant of the scope only. A value that is not a label of the enum matches
// no row: PostgreSQL would refuse the statement (22P02), and a client value
// must not give a server error.
func TestSearchFieldsSelectTheirRows(t *testing.T) {
	f := newFixture(t)
	q := NewItemSearch(f.tenant)
	for name, test := range map[string]struct {
		q    ItemSearch
		want []string
	}{
		"no filter":            {q, []string{"a1", "a2", "a3"}},
		"other tenant":         {NewItemSearch(f.other), []string{"b1"}},
		"in":                   {q.ID().In("a1", "b1", "zz"), []string{"a1"}},
		"enum eq":              {q.Status().Eq(model.ItemStatus_Archived), []string{"a3"}},
		"enum in":              {q.Status().In(model.ItemStatus_Active), []string{"a1", "a2"}},
		"enum in, not a label": {q.Status().In(model.ItemStatus_Active, "bogus"), []string{"a1", "a2"}},
		"enum eq, not a label": {q.Status().Eq("bogus"), nil},
		"contains, escaped":    {q.Name().Contains("D_C"), []string{"a2"}},
		"contains any":         {q.Name().ContainsAny("blue", "100%"), []string{"a1", "a3"}},
		"int eq":               {q.Stock().Eq(7), []string{"a3"}},
		"since":                {q.CreatedAt().Since(f.created.Add(time.Hour)), []string{"a2", "a3"}},
		"before":               {q.CreatedAt().Before(f.created.Add(time.Hour)), []string{"a1"}},
		"present":              {q.ExternalID().Present(true), []string{"a1"}},
		"absent":               {q.ExternalID().Present(false), []string{"a2", "a3"}},
		"in fold":              {q.UpperSku().In("a-2"), []string{"a2"}},
		"flag":                 {q.Expensive().Is(true), []string{"a2", "a3"}},
		"full text":            {q.FullText("a-3"), []string{"a3"}},
		"full text, blank":     {q.FullText("  "), []string{"a1", "a2", "a3"}},
		"two filters, one and": {q.Status().In(model.ItemStatus_Active).Stock().Eq(3), []string{"a1"}},
	} {
		res := run(t, f.db, test.q)
		if got := sorted(ids(res.Items)); !slices.Equal(got, test.want) || res.Total != int64(len(test.want)) {
			t.Errorf("%s: got %v (total %d), want %v", name, got, res.Total, test.want)
		}
	}
}

// A walk of one row per page on each sort, both ways, returns the rows of
// the tenant in the order of ORDER BY <column>, id on PostgreSQL: the key
// of each column kind (text, timestamptz, integer, double, enum, a nullable
// timestamp, a nullable real with NULLs last) and the default sort.
func TestSearchWalksEverySort(t *testing.T) {
	f := newFixture(t)
	for _, test := range []struct {
		sort  ItemSort
		order string
	}{
		{ItemSortName, "name"}, {ItemSortCreatedAt, "created_at"}, {ItemSortStock, "stock"},
		{ItemSortSold, "sold"}, {ItemSortPrice, "price"}, {ItemSortStatus, "status"},
		{ItemSortLocalAt, "local_at"}, {ItemSortWeight, "weight"}, {ItemSortMass, "weight"},
	} {
		for _, dir := range []search.SortDirection{search.SortAscending, search.SortDescending} {
			nulls := ""
			if test.order == "weight" {
				nulls = " NULLS LAST"
			}
			want := orderedIDs(t, f, test.order+" "+string(dir)+nulls+", id "+string(dir))
			got := walk(t, f, NewItemSearch(f.tenant).SortBy(test.sort, dir))
			if !slices.Equal(got, want) {
				t.Errorf("%s %s: walk %v, want %v", test.sort, dir, got, want)
			}
		}
	}
	if got, want := walk(t, f, NewItemSearch(f.tenant)), orderedIDs(t, f, "created_at DESC, id DESC"); !slices.Equal(
		got, want) {
		t.Errorf("default sort: walk %v, want %v", got, want)
	}
}

// walk reads every page of q, one row per page.
func walk(t *testing.T, f *fixture, q ItemSearch) []string {
	t.Helper()
	var out []string
	q = q.Limit(1)
	for range 10 {
		res := run(t, f.db, q)
		out = append(out, ids(res.Items)...)
		if res.NextCursor == "" {
			return out
		}
		q = q.After(res.NextCursor)
	}
	t.Fatalf("the walk does not end: %v", out)
	return nil
}

// orderedIDs returns the IDs of the tenant's items in the order of orderBy.
func orderedIDs(t *testing.T, f *fixture, orderBy string) []string {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(), //nolint:gosec // orderBy is a constant of the test
		"SELECT id FROM items WHERE tenant_id = $1 ORDER BY "+orderBy, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
