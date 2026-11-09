// This file is Nanostack-only (not in go-jet): the two-tenant precedence test of db/searchexec
// (R10) on every dialect. See db/UPSTREAM.md.

package searchexeccontract

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// The precedence test: every row of org-a has a twin in org-b with the same sort values and
// the same text, so each OR of a page's WHERE (the seek, the text match, ScopeOrNull) would
// let the twin through if it were not inside parentheses under the tenant scope. A walk with
// one row per page puts every row, the NULL ones too, on a page boundary.

type ptRow struct {
	ID    int64  `alias:"pt_row.id"`
	Org   string `alias:"pt_row.org"`
	Grade int64  `alias:"pt_row.grade"`
	Score *int64 `alias:"pt_row.score"`
}

// ptItem is one row of the table.
type ptItem struct {
	ptRow
	name        string
	note, owner *string
}

type ptSearch struct {
	searchexec.Query[ptRow, ptSearch, string, string]
}

type ptFixture struct {
	rows []ptItem
	spec *searchexec.Spec[ptRow, string, string]
	text func(q ptSearch) searchexec.Text[ptSearch]
}

func (s suite) newPtFixture(t *testing.T) *ptFixture {
	t.Helper()
	name := s.name("nanostack_pt_items")
	s.create(t, map[string]string{name: `id BIGINT PRIMARY KEY,
		org ` + s.text() + ` NOT NULL,
		owner ` + s.text() + `,
		grade BIGINT NOT NULL,
		score BIGINT,
		name ` + s.text() + ` NOT NULL,
		note ` + s.text()}, name)

	f := &ptFixture{}
	for i := range int64(48) {
		row := ptItem{
			ptRow: ptRow{ID: 1000 + i*7%48, Org: "org-a", Grade: i % 3},
			name:  []string{"ab-x", "zz", "cab"}[i%3],
		}
		if i%4 != 0 {
			score := i % 2
			row.Score = &score
		}
		if i%5 == 0 {
			note := "xaby"
			row.note = &note
		}
		if i%6 == 0 {
			owner := []string{"u1", "u2"}[i%12/6]
			row.owner = &owner
		}
		twin := row
		twin.ID, twin.Org = row.ID+1000, "org-b"
		for _, r := range []ptItem{row, twin} {
			s.exec(t, "INSERT INTO "+s.qualified(name)+" (id, org, owner, grade, score, name, note)"+
				" VALUES (?, ?, ?, ?, ?, ?, ?)", r.ID, r.Org, r.owner, r.Grade, r.Score, r.name, r.note)
		}
		f.rows = append(f.rows, row, twin)
	}

	id, grade, score := IntegerColumn("id"), IntegerColumn("grade"), IntegerColumn("score")
	org, owner, nameC, note := StringColumn("org"), StringColumn("owner"), StringColumn("name"), StringColumn("note")
	items := s.table(name, id, org, owner, grade, score, nameC, note)
	scoreOf := func(r ptRow) *int64 { return r.Score }
	f.spec = searchexec.DefineSpec(searchexec.SpecDef[ptRow, string, string]{
		From: func(string) keyset.Source { return items },
		Columns: ProjectionList{
			id.AS("pt_row.id"), org.AS("pt_row.org"), grade.AS("pt_row.grade"), score.AS("pt_row.score"),
		},
		Scope: []searchexec.ScopeCol[string]{
			searchexec.ScopeEq(searchexec.Value[string], org),
			searchexec.ScopeOrNull(func(string) string { return "u1" }, owner),
		},
		Keys: map[string]keyset.Key[ptRow]{
			"grade":       keyset.IntKey(grade, func(r ptRow) int64 { return r.Grade }),
			"score":       keyset.NullableIntKey(score, scoreOf),
			"score_first": keyset.NullableIntKey(score, scoreOf).NullsFirst(),
			"score_last":  keyset.NullableIntKey(score, scoreOf).NullsLast(),
		},
		Tiebreak: []keyset.Key[ptRow]{keyset.IntKey(id, func(r ptRow) int64 { return r.ID })},
		Limit:    10,
	})
	f.text = func(q ptSearch) searchexec.Text[ptSearch] { return searchexec.NewText(q.Query, nameC, note) }
	return f
}

// want is the org-a rows that the text "ab" and the owner scope keep, in the order of sorts.
func (f *ptFixture) want(sorts []search.Sort[string]) []int64 {
	var rows []ptItem
	for _, r := range f.rows {
		text := slices.Contains([]string{"ab-x", "cab"}, r.name) || r.note != nil
		if r.Org == "org-a" && text && (r.owner == nil || *r.owner == "u1") {
			rows = append(rows, r)
		}
	}
	slices.SortFunc(rows, func(a, b ptItem) int {
		last := search.SortAscending
		for _, sort := range sorts {
			last = sort.Direction
			desc := sort.Direction == search.SortDescending
			var c int
			if sort.Field == "grade" {
				c = cmp.Compare(a.Grade, b.Grade)
				if desc {
					c = -c
				}
			} else {
				nullsLast := !desc
				if sort.Field == "score_first" {
					nullsLast = false
				} else if sort.Field == "score_last" {
					nullsLast = true
				}
				c = compareNullable(a.Score, b.Score, desc, nullsLast)
			}
			if c != 0 {
				return c
			}
		}
		if last == search.SortDescending {
			return cmp.Compare(b.ID, a.ID)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func (s suite) runPrecedence(t *testing.T) {
	f := s.newPtFixture(t)
	asc, desc := search.SortAscending, search.SortDescending
	by := func(field string, d search.SortDirection) search.Sort[string] {
		return search.Sort[string]{Field: field, Direction: d}
	}
	cases := map[string][]search.Sort[string]{
		// NOT NULL keys, one direction: a row value on PostgreSQL and SQLite, the expanded
		// seek on MySQL.
		"grade ascending": {by("grade", asc)},
		// A nullable key: the expanded seek on every dialect, with the IS NULL emulation of
		// the NULL placement in MySQL's ORDER BY.
		"nullable score ascending (MySQL NULL emulation)":  {by("score", asc)},
		"nullable score descending (MySQL NULL emulation)": {by("score", desc)},
		"nullable score ascending, NULLs first":            {by("score_first", asc)},
		"nullable score descending, NULLs last":            {by("score_last", desc)},
		// Two directions: the expanded seek on every dialect.
		"grade descending, then nullable score ascending": {by("grade", desc), by("score", asc)},
	}
	for name, sorts := range cases {
		for _, limit := range []int32{1, 3} {
			t.Run(fmt.Sprintf("%s, %d per page", name, limit), func(t *testing.T) {
				if s.cfg.Flavor == CockroachDB && name != "grade ascending" {
					t.Skip("CockroachDB places NULLs first in ascending order")
				}
				want := f.want(sorts)
				require.Greater(t, len(want), 10)
				q := f.text(searchexec.New[ptSearch](f.spec, "org-a").Limit(limit)).Contains("ab")
				for _, sort := range sorts {
					q = q.SortBy(sort.Field, sort.Direction)
				}
				var got []int64
				for pages := 0; ; pages++ {
					require.LessOrEqual(t, pages, len(want), "the walk does not end")
					result, err := searchexec.Run(
						t.Context(),
						s.cfg.DB,
						f.spec,
						q.Query,
						func(r ptRow) ptRow { return r },
					)
					require.NoError(t, err)
					require.Equal(t, int64(len(want)), result.Total)
					for _, r := range result.Items {
						require.Equal(t, "org-a", r.Org, "row %d of the other tenant", r.ID)
						got = append(got, r.ID)
					}
					if !result.HasMore() {
						break
					}
					q = q.After(result.NextCursor)
				}
				require.Equal(t, want, got)
			})
		}
	}
}
