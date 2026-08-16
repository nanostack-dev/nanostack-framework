package jetx_test

import (
	"testing"
	"time"

	jet "github.com/go-jet/jet/v2/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/jetx"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// The helpers here build go-jet expressions, whose only observable form is the
// SQL they render. Every assertion therefore compares rendered SQL rather than
// expression structure, which keeps the tests pinned to what a caller can
// actually see.

var (
	colA  = jet.StringColumn("a")
	colB  = jet.StringColumn("b")
	colTS = jet.TimestampzColumn("ts")
)

// renderWhere returns the SQL a filter produces, or "" when the filter is
// absent. Absence is a documented outcome of every Build* helper, so it needs
// to be assertable alongside the SQL cases.
func renderWhere(t *testing.T, filter jet.BoolExpression) string {
	t.Helper()
	if filter == nil {
		return ""
	}
	query, _ := jet.SELECT(colA).WHERE(filter).Sql()
	return query
}

func renderArgs(t *testing.T, filter jet.BoolExpression) []any {
	t.Helper()
	if filter == nil {
		return nil
	}
	_, args := jet.SELECT(colA).WHERE(filter).Sql()
	return args
}

func TestToStringExpressionsEmptyInputIsNonNil(t *testing.T) {
	for _, values := range [][]string{nil, {}} {
		expressions := jetx.ToStringExpressions(values)
		if expressions == nil {
			t.Fatal("expected a non-nil slice even when empty")
		}
		if len(expressions) != 0 {
			t.Fatalf("expected no expressions, got %d", len(expressions))
		}
	}
}

func TestToStringExpressions(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{
			name:   "single value",
			values: []string{"one"},
			want:   "\nSELECT a AS \"a\"\nWHERE a IN ($1::text);\n",
		},
		{
			name:   "several values keep their order",
			values: []string{"one", "two", "three"},
			want:   "\nSELECT a AS \"a\"\nWHERE a IN ($1::text, $2::text, $3::text);\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expressions := jetx.ToStringExpressions(test.values)
			if len(expressions) != len(test.values) {
				t.Fatalf("expected %d expressions, got %d", len(test.values), len(expressions))
			}
			if got := renderWhere(t, colA.IN(expressions...)); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestToStringExpressionsFormatsNonStrings(t *testing.T) {
	expressions := jetx.ToStringExpressions([]int{7, 42})
	args := renderArgs(t, colA.IN(expressions...))
	if len(args) != 2 || args[0] != "7" || args[1] != "42" {
		t.Fatalf("expected [\"7\" \"42\"], got %#v", args)
	}
}

func TestToStringExpressionsFuncEmptyInputIsNonNil(t *testing.T) {
	expressions := jetx.ToStringExpressionsFunc(nil, func(v string) string { return v })
	if expressions == nil {
		t.Fatal("expected a non-nil slice even when empty")
	}
	if len(expressions) != 0 {
		t.Fatalf("expected no expressions, got %d", len(expressions))
	}
}

func TestToStringExpressionsFunc(t *testing.T) {
	type user struct{ email string }

	users := []user{{email: "a@example.com"}, {email: "b@example.com"}}
	want := []any{"a@example.com", "b@example.com"}

	expressions := jetx.ToStringExpressionsFunc(users, func(u user) string { return u.email })
	args := renderArgs(t, colA.IN(expressions...))
	if len(args) != len(want) {
		t.Fatalf("expected %d arguments, got %#v", len(want), args)
	}
	for i, wantArg := range want {
		if args[i] != wantArg {
			t.Fatalf("argument %d: expected %v, got %v", i, wantArg, args[i])
		}
	}
}

// ToStringExpressions is ToStringExpressionsFunc with a default projection, so
// the two must render identically for a slice of strings.
func TestToStringExpressionsMatchesFunc(t *testing.T) {
	values := []string{"one", "two"}

	direct := renderWhere(t, colA.IN(jetx.ToStringExpressions(values)...))
	projected := renderWhere(
		t,
		colA.IN(jetx.ToStringExpressionsFunc(values, func(v string) string { return v })...),
	)
	if direct != projected {
		t.Fatalf("expected identical SQL, got %q and %q", direct, projected)
	}
}

func TestOrderBy(t *testing.T) {
	tests := []struct {
		name      string
		direction search.SortDirection
		want      string
	}{
		{name: "ascending", direction: search.SortAscending, want: "\nSELECT a AS \"a\"\nORDER BY a ASC;\n"},
		{name: "descending", direction: search.SortDescending, want: "\nSELECT a AS \"a\"\nORDER BY a DESC;\n"},
		{name: "unknown direction falls back to ascending", direction: "sideways", want: "\nSELECT a AS \"a\"\nORDER BY a ASC;\n"},
		{name: "empty direction falls back to ascending", direction: "", want: "\nSELECT a AS \"a\"\nORDER BY a ASC;\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, _ := jet.SELECT(colA).ORDER_BY(jetx.OrderBy(colA, test.direction)).Sql()
			if query != test.want {
				t.Fatalf("expected %q, got %q", test.want, query)
			}
		})
	}
}

func TestBuildStringArrayFilter(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "no values is no filter", values: nil, want: ""},
		{name: "empty slice is no filter", values: []string{}, want: ""},
		{
			name:   "a single value compares directly",
			values: []string{"one"},
			want:   "\nSELECT a AS \"a\"\nWHERE a = $1::text;\n",
		},
		{
			name:   "several values use IN",
			values: []string{"one", "two"},
			want:   "\nSELECT a AS \"a\"\nWHERE a IN ($1::text, $2::text);\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := renderWhere(t, jetx.BuildStringArrayFilter(colA, test.values)); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
			// BuildIDFilter is BuildStringArrayFilter under another name.
			if got := renderWhere(t, jetx.BuildIDFilter(colA, test.values)); got != test.want {
				t.Fatalf("BuildIDFilter: expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestBuildSubstringFilter(t *testing.T) {
	tests := []struct {
		name     string
		columns  []jet.ColumnString
		term     string
		want     string
		wantArgs []any
	}{
		{name: "empty term is no filter", columns: []jet.ColumnString{colA}, term: "", want: ""},
		{name: "no columns is no filter", columns: nil, term: "term", want: ""},
		{
			name:     "one column",
			columns:  []jet.ColumnString{colA},
			term:     "term",
			want:     "\nSELECT a AS \"a\"\nWHERE a LIKE $1::text;\n",
			wantArgs: []any{"%term%"},
		},
		{
			name:     "several columns are ORed",
			columns:  []jet.ColumnString{colA, colB},
			term:     "term",
			want:     "\nSELECT a AS \"a\"\nWHERE (a LIKE $1::text) OR (b LIKE $2::text);\n",
			wantArgs: []any{"%term%", "%term%"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter := jetx.BuildSubstringFilter(test.columns, test.term)
			if got := renderWhere(t, filter); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
			args := renderArgs(t, filter)
			if len(args) != len(test.wantArgs) {
				t.Fatalf("expected %d arguments, got %#v", len(test.wantArgs), args)
			}
			for i, want := range test.wantArgs {
				if args[i] != want {
					t.Fatalf("argument %d: expected %v, got %v", i, want, args[i])
				}
			}
		})
	}
}

func TestBuildDateRangeFilter(t *testing.T) {
	from := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	to := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

	tests := []struct {
		name     string
		from, to *time.Time
		want     string
	}{
		{name: "no bounds is no filter", want: ""},
		{
			name: "lower bound is inclusive",
			from: &from,
			want: "\nSELECT a AS \"a\"\nWHERE ts >= $1::timestamp with time zone;\n",
		},
		{
			name: "upper bound is inclusive",
			to:   &to,
			want: "\nSELECT a AS \"a\"\nWHERE ts <= $1::timestamp with time zone;\n",
		},
		{
			name: "both bounds are ANDed",
			from: &from,
			to:   &to,
			want: "\nSELECT a AS \"a\"\nWHERE (ts >= $1::timestamp with time zone) AND (ts <= $2::timestamp with time zone);\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter := jetx.BuildDateRangeFilter(colTS, test.from, test.to)
			if got := renderWhere(t, filter); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestCombineFilters(t *testing.T) {
	tests := []struct {
		name    string
		filters []jet.BoolExpression
		wantAnd string
		wantOr  string
	}{
		{name: "nothing to combine", filters: nil},
		{name: "only absent filters", filters: []jet.BoolExpression{nil, nil}},
		{
			name:    "a single filter is returned as-is",
			filters: []jet.BoolExpression{colA.EQ(jet.String("x"))},
			wantAnd: "\nSELECT a AS \"a\"\nWHERE a = $1::text;\n",
			wantOr:  "\nSELECT a AS \"a\"\nWHERE a = $1::text;\n",
		},
		{
			name:    "absent filters are skipped",
			filters: []jet.BoolExpression{nil, colA.EQ(jet.String("x")), nil},
			wantAnd: "\nSELECT a AS \"a\"\nWHERE a = $1::text;\n",
			wantOr:  "\nSELECT a AS \"a\"\nWHERE a = $1::text;\n",
		},
		{
			name: "several filters fold left to right",
			filters: []jet.BoolExpression{
				colA.EQ(jet.String("x")),
				colB.EQ(jet.String("y")),
				colA.EQ(jet.String("z")),
			},
			wantAnd: "\nSELECT a AS \"a\"\nWHERE ((a = $1::text) AND (b = $2::text)) AND (a = $3::text);\n",
			wantOr:  "\nSELECT a AS \"a\"\nWHERE ((a = $1::text) OR (b = $2::text)) OR (a = $3::text);\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := renderWhere(t, jetx.CombineFilters(test.filters...)); got != test.wantAnd {
				t.Fatalf("CombineFilters: expected %q, got %q", test.wantAnd, got)
			}
			if got := renderWhere(t, jetx.CombineFiltersWithOr(test.filters...)); got != test.wantOr {
				t.Fatalf("CombineFiltersWithOr: expected %q, got %q", test.wantOr, got)
			}
		})
	}
}
