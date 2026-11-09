//go:build integration

package repotest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// These tests run the builders of row.go on PostgreSQL: a Row of its own
// selects its columns, a joined column fills its field, and a walk of one
// row per page on a joined sort returns every row of the tenant once, in
// the order PostgreSQL gives.

func runSummary(t *testing.T, f *fixture, q ItemSummarySearch) search.Result[itemSummary] {
	t.Helper()
	res, err := searchexec.Run(context.Background(), f.db, itemSummarySearch, q.Query,
		func(r itemSummary) itemSummary { return r })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

func runOwner(t *testing.T, f *fixture, q ItemWithOwnerSearch) search.Result[itemWithOwner] {
	t.Helper()
	res, err := searchexec.Run(context.Background(), f.db, itemWithOwnerSearch, q.Query,
		func(r itemWithOwner) itemWithOwner { return r })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

// The column subset fills each field, the joined tenants.name included.
func TestRowSelectsItsColumns(t *testing.T) {
	f := newFixture(t)
	res := runSummary(t, f, NewItemSummarySearch(f.tenant).SortBy(ItemSummarySortName, search.SortAscending))
	got := res.Items
	want := []itemSummary{
		{ID: "a3", Name: "Blue", CreatedAt: f.created.Add(2 * time.Hour), TenantName: "tenant a"},
		{ID: "a1", Name: "Red 100% wool", CreatedAt: f.created, TenantName: "tenant a"},
		{ID: "a2", Name: "Red_cotton", CreatedAt: f.created.Add(time.Hour), TenantName: "tenant a"},
	}
	if len(got) != len(want) || res.Total != 3 {
		t.Fatalf("got %+v (total %d), want %+v", got, res.Total, want)
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Name != want[i].Name || got[i].TenantName != want[i].TenantName ||
			!got[i].CreatedAt.Equal(want[i].CreatedAt) {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A LEFT-joined column is nil when no row matches: a2 is not active, so
// the join reads no tenant for it. The Present filter on the joined column
// keeps the rows that have one.
func TestRowReadsALeftJoinedColumn(t *testing.T) {
	f := newFixture(t)
	res := runOwner(t, f, NewItemWithOwnerSearch(f.tenant))
	owners := map[string]*string{}
	for _, r := range res.Items {
		owners[r.Items.ID] = r.OwnerName
	}
	if len(owners) != 3 || owners["a2"] != nil || owners["a1"] == nil || *owners["a1"] != "tenant a" {
		t.Errorf("owners: %v", owners)
	}
	present := runOwner(t, f, NewItemWithOwnerSearch(f.tenant).Name().Present(true))
	if got := sorted(ownerIDs(present.Items)); !slices.Equal(got, []string{"a1", "a3"}) {
		t.Errorf("present: %v", got)
	}
}

// An embedded model with an alias tag reads the columns of the aliased
// instance, and an embedded model without a tag the columns of the table.
func TestRowEmbedsAnAliasedModel(t *testing.T) {
	f := newFixture(t)
	res, err := searchexec.Run(context.Background(), f.db, itemOfOwnerSearch,
		NewItemOfOwnerSearch(f.other).Query, func(r itemOfOwner) itemOfOwner { return r })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("rows: %+v", res.Items)
	}
	r := res.Items[0]
	if r.Items.ID != "b1" || r.Items.Name != "Red" || r.Tenants.ID != f.other || r.Tenants.Name != "tenant b" {
		t.Errorf("row: %+v", r)
	}
}

// A walk of one row per page on the sorts of joined columns, both ways,
// returns the rows in the order of ORDER BY <column>, items.id on
// PostgreSQL: tenants.name through an INNER JOIN, and through a LEFT JOIN
// with NULLs last, where the walk crosses from the names to the NULLs.
func TestRowWalksJoinedSorts(t *testing.T) {
	f := newFixture(t)
	for _, dir := range []search.SortDirection{search.SortAscending, search.SortDescending} {
		d := string(dir)
		want := orderedJoinIDs(t, f, "INNER JOIN tenants ON tenants.id = items.tenant_id",
			"tenants.name "+d+", items.id "+d)
		got := walkSummary(t, f, NewItemSummarySearch(f.tenant).SortBy(ItemSummarySortTenantName, dir))
		if !slices.Equal(got, want) {
			t.Errorf("tenant_name %s: walk %v, want %v", dir, got, want)
		}
		want = orderedJoinIDs(t, f, "LEFT JOIN tenants ON tenants.id = items.tenant_id AND items.active",
			"tenants.name "+d+" NULLS LAST, items.id "+d)
		got = walkOwner(t, f, NewItemWithOwnerSearch(f.tenant).SortBy(ItemWithOwnerSortOwnerName, dir))
		if !slices.Equal(got, want) {
			t.Errorf("owner_name %s: walk %v, want %v", dir, got, want)
		}
	}
}

func walkSummary(t *testing.T, f *fixture, q ItemSummarySearch) []string {
	t.Helper()
	var out []string
	q = q.Limit(1)
	for range 10 {
		res := runSummary(t, f, q)
		for _, r := range res.Items {
			out = append(out, r.ID)
		}
		if res.NextCursor == "" {
			return out
		}
		q = q.After(res.NextCursor)
	}
	t.Fatalf("the walk does not end: %v", out)
	return nil
}

func walkOwner(t *testing.T, f *fixture, q ItemWithOwnerSearch) []string {
	t.Helper()
	var out []string
	q = q.Limit(1)
	for range 10 {
		res := runOwner(t, f, q)
		out = append(out, ownerIDs(res.Items)...)
		if res.NextCursor == "" {
			return out
		}
		q = q.After(res.NextCursor)
	}
	t.Fatalf("the walk does not end: %v", out)
	return nil
}

func ownerIDs(rows []itemWithOwner) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Items.ID
	}
	return out
}

// orderedJoinIDs returns the IDs of the tenant's items, joined by join, in
// the order of orderBy.
func orderedJoinIDs(t *testing.T, f *fixture, join, orderBy string) []string {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(), //nolint:gosec // join and orderBy are constants of the test
		"SELECT items.id FROM items "+join+" WHERE items.tenant_id = $1 ORDER BY "+orderBy, f.tenant)
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
