package transactor_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
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

func normalize(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

func TestSortColumns(t *testing.T) {
	// sortUnmapped is deliberately absent — these tests cover what happens to a
	// sort field with no column.
	//nolint:exhaustive // the missing key is the subject under test
	columns := map[sortField]jet.Column{
		sortByName:    colName,
		sortByCreated: colCreated,
	}

	tests := []struct {
		name  string
		sorts []search.Sort[sortField]
		want  int
	}{
		{name: "no sorts yields no clauses", sorts: nil, want: 0},
		{
			name:  "a mapped field yields a clause",
			sorts: []search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}},
			want:  1,
		},
		{
			name: "order is preserved across several fields",
			sorts: []search.Sort[sortField]{
				{Field: sortByCreated, Direction: search.SortDescending},
				{Field: sortByName, Direction: search.SortAscending},
			},
			want: 2,
		},
		{
			// The switch this replaces silently ignored unknown fields too, but
			// only because every case was written by hand.
			name:  "an unmapped field is skipped rather than failing",
			sorts: []search.Sort[sortField]{{Field: sortUnmapped, Direction: search.SortAscending}},
			want:  0,
		},
		{
			name: "unmapped fields are skipped without dropping mapped ones",
			sorts: []search.Sort[sortField]{
				{Field: sortUnmapped, Direction: search.SortAscending},
				{Field: sortByName, Direction: search.SortAscending},
			},
			want: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := transactor.SortColumns(tc.sorts, columns); len(got) != tc.want {
				t.Fatalf("SortColumns() gave %d clauses, want %d", len(got), tc.want)
			}
		})
	}
}

func TestSortColumnsDirection(t *testing.T) {
	//nolint:exhaustive // only the mapped field matters here
	columns := map[sortField]jet.Column{sortByName: colName}

	for _, tc := range []struct {
		name      string
		direction search.SortDirection
		want      string
	}{
		{name: "ascending", direction: search.SortAscending, want: "ASC"},
		{name: "descending", direction: search.SortDescending, want: "DESC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clauses := transactor.SortColumns(
				[]search.Sort[sortField]{{Field: sortByName, Direction: tc.direction}}, columns,
			)
			sql := normalize(jet.SELECT(colName).FROM(testTable).ORDER_BY(clauses...).DebugSql())
			if !strings.Contains(sql, "ORDER BY widgets.name "+tc.want) {
				t.Fatalf("SQL = %q, want ORDER BY ... %s", sql, tc.want)
			}
		})
	}
}

// TestPageRunWithoutFrom pins the one misuse the builder can detect: Run
// without From would otherwise dereference a nil table.
func TestPageRunWithoutFrom(t *testing.T) {
	result := transactor.Page(nil, toDomain, colName).Run(t.Context(), search.Pagination{Limit: 10})
	if _, err := result.Value(); !errors.Is(err, transactor.ErrPageSourceMissing) {
		t.Fatalf("Run() without From = %v, want ErrPageSourceMissing", err)
	}
}

// The transactor's sentinels are the values of db/keyset, so errors.Is gives
// one answer whichever package a caller checks against.
func TestPageErrorsAreTheKeysetErrors(t *testing.T) {
	if !errors.Is(transactor.ErrPageSourceMissing, keyset.ErrPageSourceMissing) ||
		!errors.Is(transactor.ErrPageOrderConflict, keyset.ErrPageOrderConflict) {
		t.Fatal("the page errors of transactor are not the values of db/keyset")
	}
}
