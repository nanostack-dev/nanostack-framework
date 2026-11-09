//go:build integration

package repotest

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// These tests run the searches of tenant.go through the methods of
// ItemRepository, generated except SearchAssignableLabels, on the two
// tenants of the fixture.

// seedTenantRows adds labels (one shared, one per tenant) with their uses,
// a license per tenant on a1 and a2, and the events of a1, a3 and b1.
func seedTenantRows(t *testing.T, f *fixture) {
	t.Helper()
	for _, stmt := range []string{
		fmt.Sprintf(`INSERT INTO labels (id, tenant_id, name) VALUES ('s1', NULL, 'shared'),
			('a1', '%s', 'alpha'), ('b1', '%s', 'beta')`, f.tenant, f.other),
		`INSERT INTO label_uses (label_id, kind) VALUES ('s1', 'x'), ('a1', 'y'), ('b1', 'x')`,
		fmt.Sprintf(`INSERT INTO item_licenses (item_id, tenant_id, plan) VALUES ('a1', '%s', 'pro'),
			('a2', '%s', 'other tenant')`, f.tenant, f.other),
		`INSERT INTO item_events (item_id) VALUES ('a1'), ('a1'), ('a3'), ('b1'), ('b1'), ('b1'), ('b1'), ('b1')`,
	} {
		if _, err := f.db.ExecContext(context.Background(), stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func labelIDs(res search.Result[Label], err error) (string, error) {
	ids := make([]string, len(res.Items))
	for i, l := range res.Items {
		ids[i] = l.ID
	}
	return strings.Join(ids, ","), err
}

// Shaped like anchor #8 (ScopeNull and EXISTS) and #9 (ScopeOrNull and
// EXISTS): the shared rows are read with or without the tenant's, never
// another tenant's, and the EXISTS filter keeps the rows with a child of
// that kind.
func TestSharedRowsAndExists(t *testing.T) {
	f := newFixture(t)
	seedTenantRows(t, f)
	ctx := context.Background()
	for name, run := range map[string]func() (string, error){
		"managed": func() (string, error) { return labelIDs(f.repo.SearchManagedLabels(ctx, NewManagedLabelSearch())) },
		"managed, used as x": func() (string, error) {
			return labelIDs(f.repo.SearchManagedLabels(ctx, NewManagedLabelSearch().LabelUsesKind().In("x")))
		},
		"managed, used as y": func() (string, error) {
			return labelIDs(f.repo.SearchManagedLabels(ctx, NewManagedLabelSearch().LabelUsesKind().In("y")))
		},
		"assignable": func() (string, error) {
			return labelIDs(f.repo.SearchAssignableLabels(ctx, NewAssignableLabelSearch(f.tenant)))
		},
		"assignable, other tenant": func() (string, error) {
			return labelIDs(f.repo.SearchAssignableLabels(ctx, NewAssignableLabelSearch(f.other)))
		},
		"assignable, used as x": func() (string, error) {
			return labelIDs(f.repo.SearchAssignableLabels(ctx, NewAssignableLabelSearch(f.tenant).LabelUsesKind().In("x")))
		},
		"assignable, no kind": func() (string, error) {
			return labelIDs(f.repo.SearchAssignableLabels(ctx, NewAssignableLabelSearch(f.tenant).LabelUsesKind().In()))
		},
	} {
		want := map[string]string{
			"managed": "s1", "managed, used as x": "s1", "managed, used as y": "",
			"assignable": "a1,s1", "assignable, other tenant": "b1,s1", "assignable, used as x": "s1",
			"assignable, no kind": "",
		}[name]
		got, err := run()
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, want)
		}
	}
}

// legacyLicensedItems is the hand-written query the generated
// SearchLicensedItems replaces: the license of the tenant in the ON clause.
func legacyLicensedItems(t *testing.T, db *sql.DB, tenant uuid.UUID) []itemWithLicense {
	t.Helper()
	stmt := postgres.SELECT(table.Items.AllColumns, table.ItemLicenses.Plan).
		FROM(table.Items.LEFT_JOIN(table.ItemLicenses, table.ItemLicenses.ItemID.EQ(table.Items.ID).
			AND(table.ItemLicenses.TenantID.EQ(postgres.UUID(tenant))))).
		WHERE(table.Items.TenantID.EQ(postgres.UUID(tenant))).
		ORDER_BY(table.Items.ID.ASC()) // no DefaultSort: the tiebreak orders the rows
	var rows []itemWithLicense
	if err := stmt.QueryContext(context.Background(), db, &rows); err != nil {
		t.Fatalf("legacy: %v", err)
	}
	return rows
}

// legacyCountedItems is today's hand-written shape of a counted sort: the
// count of each row's children, ordered by it.
func legacyCountedItems(t *testing.T, db *sql.DB, tenant uuid.UUID) []itemWithEvents {
	t.Helper()
	count := postgres.IntExp(postgres.SELECT(postgres.COUNT(table.ItemEvents.ID)).FROM(table.ItemEvents).
		WHERE(table.ItemEvents.ItemID.EQ(table.Items.ID)))
	stmt := postgres.SELECT(table.Items.AllColumns, count.AS("item_with_events.event_count")).
		FROM(table.Items).
		WHERE(table.Items.TenantID.EQ(postgres.UUID(tenant))).
		ORDER_BY(postgres.IntegerColumn("item_with_events.event_count").DESC(), table.Items.ID.ASC())
	var rows []itemWithEvents
	if err := stmt.QueryContext(context.Background(), db, &rows); err != nil {
		t.Fatalf("legacy: %v", err)
	}
	return rows
}

// The generated methods return what their hand-written Legacy copies
// return on the same rows: anchor #22's LEFT JOIN with the tenant in the ON
// clause keeps the items with no license of that tenant, and the computed
// key orders and fills the count as the correlated subquery does.
func TestGeneratedMethodsMatchLegacy(t *testing.T) {
	f := newFixture(t)
	seedTenantRows(t, f)
	ctx := context.Background()
	for _, tenant := range []uuid.UUID{f.tenant, f.other} {
		licensed, err := f.repo.SearchLicensedItems(ctx, NewLicensedItemSearch(tenant))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := plans(licensed.Items), plans(legacyLicensedItems(t, f.db, tenant)); got != want {
			t.Errorf("licensed items of %s: got %s, legacy %s", tenant, got, want)
		}
		counted, err := f.repo.SearchCountedItems(ctx, NewCountedItemSearch(tenant))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := counts(counted.Items), counts(legacyCountedItems(t, f.db, tenant)); got != want {
			t.Errorf("counted items of %s: got %s, legacy %s", tenant, got, want)
		}
	}
	// The count is selected for every order, so a page sorted by name
	// carries it too.
	byName, err := f.repo.SearchCountedItems(ctx,
		NewCountedItemSearch(f.tenant).SortBy(CountedItemSortName, search.SortAscending))
	if got := counts(byName.Items); err != nil || got != "a3:1 a1:2 a2:0" {
		t.Errorf("by name: got %s, %v", got, err)
	}
	// A walk of one row per page on the count, both ways, has every row
	// once, in order: the cursor reads the computed key.
	for _, dir := range []search.SortDirection{search.SortDescending, search.SortAscending} {
		q := NewCountedItemSearch(f.tenant).SortBy(CountedItemSortEventCount, dir).Limit(1)
		var walked []string
		for range 4 {
			res, walkErr := f.repo.SearchCountedItems(ctx, q)
			if walkErr != nil {
				t.Fatal(walkErr)
			}
			walked = append(walked, strings.Fields(counts(res.Items))...)
			if res.NextCursor == "" {
				break
			}
			q = q.After(res.NextCursor)
		}
		want := []string{"a1:2", "a3:1", "a2:0"}
		if dir == search.SortAscending {
			slices.Reverse(want)
		}
		if !slices.Equal(walked, want) {
			t.Errorf("walk %s: got %v, want %v", dir, walked, want)
		}
	}
}

// PostgreSQL computes the count after the sort, under the Limit, when the
// order does not use it, so a page sorted by name counts for the rows it
// returns only.
func TestCountAboveLimitWhenNotSorted(t *testing.T) {
	f := newFixture(t)
	seedTenantRows(t, f)
	rec := &recorder{DB: f.db}
	repo := &itemRepositoryImpl{itemsBase: newItemsBase(rec)}
	ctx := context.Background()
	for _, test := range []struct {
		sort  CountedItemSort
		above bool
	}{{CountedItemSortName, true}, {CountedItemSortEventCount, false}} {
		rec.last = ""
		if _, err := repo.SearchCountedItems(ctx, NewCountedItemSearch(f.tenant).SortBy(test.sort, search.SortAscending)); err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		rows, err := f.db.QueryContext(ctx, "EXPLAIN (VERBOSE, COSTS OFF) "+rec.last, rec.args...)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err = rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(line + "\n")
		}
		_ = rows.Close()
		out := plan.String()
		// The Sort node outputs the count only when it must compute it to
		// sort: else the Result above the Sort computes it for the rows
		// that the Limit pulls.
		at := strings.Index(out, "Sort\n")
		output := strings.Index(out[max(at, 0):], "Output:")
		if at < 0 || output < 0 {
			t.Fatalf("%s: no Sort node in\n%s", test.sort, out)
		}
		line, _, _ := strings.Cut(out[at+output:], "\n")
		if got := !strings.Contains(line, "SubPlan"); got != test.above {
			t.Errorf("%s: the count is computed after the sort: %v, want %v\n%s", test.sort, got, test.above, out)
		}
	}
}

// recorder keeps the last page statement it runs.
type recorder struct {
	qrm.DB
	last string
	args []any
}

func (r *recorder) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "LIMIT") {
		r.last, r.args = query, args
	}
	return r.DB.QueryContext(ctx, query, args...)
}

func plans(rows []itemWithLicense) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		plan := "-"
		if r.Plan != nil {
			plan = *r.Plan
		}
		out[i] = r.ID + ":" + plan
	}
	return strings.Join(out, " ")
}

func counts(rows []itemWithEvents) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%s:%d", r.ID, r.EventCount)
	}
	return strings.Join(out, " ")
}
