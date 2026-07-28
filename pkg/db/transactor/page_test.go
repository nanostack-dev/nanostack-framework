package transactor_test

import (
	"strings"
	"testing"

	jet "github.com/go-jet/jet/v2/postgres"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
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

func builder() *transactor.PageBuilder[row, domain] {
	return transactor.Page(nil, toDomain, colName, colTenant).From(testTable)
}

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
			sql := normalize(builder().OrderBy(clauses...).
				PageStatementForTest(search.Pagination{Limit: 10}).DebugSql())
			if !strings.Contains(sql, "ORDER BY widgets.name "+tc.want) {
				t.Fatalf("page SQL = %q, want ORDER BY ... %s", sql, tc.want)
			}
		})
	}
}

func TestPageCountStatement(t *testing.T) {
	t.Run("counts rows, not columns, and ignores ordering", func(t *testing.T) {
		sql := normalize(builder().
			OrderBy(colName.ASC()).
			CountStatementForTest().DebugSql())
		if !strings.Contains(sql, "SELECT COUNT(*)") {
			t.Fatalf("count SQL = %q, want SELECT COUNT(*)", sql)
		}
		if strings.Contains(sql, "ORDER BY") {
			t.Fatalf("count SQL = %q, must not order — it only needs the total", sql)
		}
		if strings.Contains(sql, "LIMIT") {
			t.Fatalf("count SQL = %q, must not paginate — Total covers every match", sql)
		}
	})

	t.Run("applies the same filter as the page", func(t *testing.T) {
		where := colTenant.EQ(jet.String("tenant1"))
		count := normalize(builder().Where(where).CountStatementForTest().DebugSql())
		page := normalize(builder().Where(where).
			PageStatementForTest(search.Pagination{Limit: 10}).DebugSql())
		for _, sql := range []string{count, page} {
			if !strings.Contains(sql, "widgets.tenant_id = 'tenant1'") {
				t.Fatalf("SQL = %q, want the filter applied", sql)
			}
		}
	})

	t.Run("a nil filter selects everything", func(t *testing.T) {
		if sql := normalize(builder().CountStatementForTest().DebugSql()); strings.Contains(sql, "WHERE") {
			t.Fatalf("count SQL = %q, want no WHERE when no filter is set", sql)
		}
	})
}

func TestPagePageStatement(t *testing.T) {
	t.Run("selects the projected columns", func(t *testing.T) {
		sql := normalize(builder().PageStatementForTest(search.Pagination{Limit: 10}).DebugSql())
		for _, want := range []string{"widgets.name", "widgets.tenant_id"} {
			if !strings.Contains(sql, want) {
				t.Fatalf("page SQL = %q, want %s projected", sql, want)
			}
		}
	})

	t.Run("applies limit and offset", func(t *testing.T) {
		sql := normalize(builder().
			PageStatementForTest(search.Pagination{Limit: 25, Offset: 50}).DebugSql())
		if !strings.Contains(sql, "LIMIT 25") || !strings.Contains(sql, "OFFSET 50") {
			t.Fatalf("page SQL = %q, want LIMIT 25 / OFFSET 50", sql)
		}
	})

	t.Run("orders by every clause in the order given", func(t *testing.T) {
		sql := normalize(builder().
			OrderBy(colCreated.DESC(), colName.ASC()).
			PageStatementForTest(search.Pagination{Limit: 10}).DebugSql())
		if !strings.Contains(sql, "ORDER BY widgets.created_at DESC, widgets.name ASC") {
			t.Fatalf("page SQL = %q, want both clauses in order", sql)
		}
	})
}

// TestPageRunWithoutFrom pins the one misuse the builder can detect: Run
// without From would otherwise dereference a nil table.
func TestPageRunWithoutFrom(t *testing.T) {
	result := transactor.Page(nil, toDomain, colName).Run(t.Context(), search.Pagination{Limit: 10})
	if _, err := result.Value(); err == nil {
		t.Fatal("Run() without From returned no error")
	}
}
