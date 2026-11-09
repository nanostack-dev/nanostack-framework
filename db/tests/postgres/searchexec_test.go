// This file is Nanostack-only (not in go-jet): the db/searchexec core against PostgreSQL, on a
// SpecDef written by hand as nano gen repo writes it. See db/UPSTREAM.md.

package postgres

import (
	"cmp"
	"context"
	"crypto/rand"
	"fmt"
	mathrand "math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// seRow is the row of the searches. Its fields read the aliases of seFixture.columns, so the
// scan does not depend on the random table names.
type seRow struct {
	ID    int64   `alias:"se_row.id"`
	Org   string  `alias:"se_row.org"`
	Team  string  `alias:"se_row.team_id"`
	Owner *string `alias:"se_row.owner"`
	Space string  `alias:"se_row.workspace"`
	Rank  int64   `alias:"se_row.rank"`
	Score *int64  `alias:"se_row.score"`
	Gone  bool    `alias:"se_row.deleted"`
	// Weight is filled only by the hand-written computed key of TestSearchexecHandWrittenSort.
	Weight int64 `alias:"se_row.weight"`
}

type seSort string

const (
	seRank    seSort = "rank"
	seScore   seSort = "score"
	seScoreNF seSort = "score_nulls_first"
	seScoreNL seSort = "score_nulls_last"
)

// seScope is the scope struct of the item search, as nano gen repo writes it.
type seScope struct {
	_     struct{} // keyed fields only
	Org   string   // items.org and teams.org
	Team  string   // items.team_id
	Owner string   // items.owner, or NULL
}

type seItemSearch struct {
	searchexec.Query[seRow, seItemSearch, seSort, seScope]
}

// seOrgSearch has one scope value, the organization.
type seOrgSearch struct {
	searchexec.Query[seRow, seOrgSearch, seSort, string]
}

type seWorkspaceKey struct{}

// seWorkspace is the context scope: the workspace of the context, TRUE when the context has
// none, and nil when the context asks for it (a broken scope function).
func seWorkspace(ctx context.Context, column ColumnString) BoolExpression {
	switch v, _ := ctx.Value(seWorkspaceKey{}).(string); v {
	case "":
		return Bool(true)
	case "nil":
		return nil
	default:
		return column.EQ(String(v))
	}
}

type seFixture struct {
	rows []seRow

	items, teams                               Table
	id, rank, score                            ColumnInteger
	org, team, owner, workspace, teamID, teamO ColumnString
	deleted                                    ColumnBool

	items2Teams ReadableTable
	columns     ProjectionList
	keys        map[seSort]keyset.Key[seRow]
	tiebreak    []keyset.Key[seRow]

	itemSpec *searchexec.Spec[seRow, seSort, seScope]
	orgSpec  *searchexec.Spec[seRow, seSort, string]
}

var seTeams = map[string]string{"t1": "org-a", "t2": "org-a", "t3": "org-b"}

// newSeFixture creates the two tables with new random names (so the statement-cache runs and
// a shuffled order never meet a cached statement of a dropped table) and fills them. The data
// has few distinct sort values, so ties dominate, and NULL scores and owners. Some rows name
// a team of another organization, so only the scope on teams.org drops them.
func newSeFixture(t *testing.T) *seFixture {
	t.Helper()

	suffix := strings.ToLower(rand.Text())
	teamsName, itemsName := "nanostack_se_teams_"+suffix, "nanostack_se_items_"+suffix
	for _, stmt := range []string{
		"CREATE TABLE test_sample." + teamsName + " (id TEXT PRIMARY KEY, org TEXT NOT NULL)",
		"CREATE TABLE test_sample." + itemsName + ` (
			id BIGINT PRIMARY KEY,
			org TEXT NOT NULL,
			team_id TEXT NOT NULL REFERENCES test_sample.` + teamsName + ` (id),
			owner TEXT,
			workspace TEXT NOT NULL,
			rank BIGINT NOT NULL,
			score BIGINT,
			deleted BOOLEAN NOT NULL
		)`,
	} {
		_, err := db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		for _, name := range []string{itemsName, teamsName} {
			_, err := db.ExecContext(context.Background(), "DROP TABLE test_sample."+name)
			require.NoError(t, err)
		}
	})
	for id, org := range seTeams {
		_, err := db.ExecContext(ctx, "INSERT INTO test_sample."+teamsName+" (id, org) VALUES ($1, $2)", id, org)
		require.NoError(t, err)
	}

	f := &seFixture{}
	rng := mathrand.New(mathrand.NewPCG(6, 9))
	used := map[int64]bool{}
	for range 160 {
		row := seRow{Org: "org-a", Team: "t1", Space: "w1", Rank: rng.Int64N(3)}
		// Random ids, so the tiebreak order is unrelated to the insertion order.
		for row.ID == 0 || used[row.ID] {
			row.ID = rng.Int64N(1_000_000) + 1
		}
		used[row.ID] = true
		switch rng.IntN(10) {
		case 0:
			row.Org, row.Team = "org-b", "t3"
		case 1:
			row.Team = "t2"
		case 2:
			row.Team = "t3" // a team of org-b: teams.org = org-b
		}
		if owner := []string{"u1", "u2", ""}[rng.IntN(3)]; owner != "" {
			row.Owner = &owner
		}
		if rng.IntN(2) == 0 {
			row.Space = "w2"
		}
		if rng.IntN(3) > 0 {
			score := rng.Int64N(3)
			row.Score = &score
		}
		row.Gone = rng.IntN(10) == 0
		_, err := db.ExecContext(ctx, "INSERT INTO test_sample."+itemsName+
			" (id, org, team_id, owner, workspace, rank, score, deleted) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
			row.ID, row.Org, row.Team, row.Owner, row.Space, row.Rank, row.Score, row.Gone)
		require.NoError(t, err)
		f.rows = append(f.rows, row)
	}

	f.id, f.rank, f.score = IntegerColumn("id"), IntegerColumn("rank"), IntegerColumn("score")
	f.org, f.team, f.owner = StringColumn("org"), StringColumn("team_id"), StringColumn("owner")
	f.workspace, f.deleted = StringColumn("workspace"), BoolColumn("deleted")
	f.items = NewTable("test_sample", itemsName, "",
		f.id, f.org, f.team, f.owner, f.workspace, f.rank, f.score, f.deleted)
	f.teamID, f.teamO = StringColumn("id"), StringColumn("org")
	f.teams = NewTable("test_sample", teamsName, "", f.teamID, f.teamO)
	f.items2Teams = f.items.INNER_JOIN(f.teams, f.teamID.EQ(f.team))
	f.columns = ProjectionList{
		f.id.AS("se_row.id"), f.org.AS("se_row.org"), f.team.AS("se_row.team_id"), f.owner.AS("se_row.owner"),
		f.workspace.AS("se_row.workspace"), f.rank.AS("se_row.rank"), f.score.AS("se_row.score"),
		f.deleted.AS("se_row.deleted"),
	}
	score := func(r seRow) *int64 { return r.Score }
	f.keys = map[seSort]keyset.Key[seRow]{
		seRank:    keyset.IntKey(f.rank, func(r seRow) int64 { return r.Rank }),
		seScore:   keyset.NullableIntKey(f.score, score),
		seScoreNF: keyset.NullableIntKey(f.score, score).NullsFirst(),
		seScoreNL: keyset.NullableIntKey(f.score, score).NullsLast(),
	}
	f.tiebreak = []keyset.Key[seRow]{keyset.IntKey(f.id, func(r seRow) int64 { return r.ID })}
	f.itemSpec = searchexec.DefineSpec(f.itemDef())
	f.orgSpec = searchexec.DefineSpec(searchexec.SpecDef[seRow, seSort, string]{
		From:    func(string) keyset.Source { return f.items },
		Columns: f.columns,
		Scope: []searchexec.ScopeCol[string]{
			searchexec.ScopeEq(searchexec.Value[string], f.org),
			searchexec.ScopeNull[string](f.owner),
		},
		Keys:     f.keys,
		Tiebreak: f.tiebreak,
		Limit:    50,
	})
	return f
}

// itemDef is the item search: one field scopes items.org and teams.org, the team is scoped
// on items, the owner may be NULL, the workspace comes from the context, and deleted rows
// never show.
func (f *seFixture) itemDef() searchexec.SpecDef[seRow, seSort, seScope] {
	return searchexec.SpecDef[seRow, seSort, seScope]{
		From:    func(seScope) keyset.Source { return f.items2Teams },
		Columns: f.columns,
		Scope: []searchexec.ScopeCol[seScope]{
			searchexec.ScopeEq(func(s seScope) string { return s.Org }, f.org, f.teamO),
			searchexec.ScopeEq(func(s seScope) string { return s.Team }, f.team),
			searchexec.ScopeOrNull(func(s seScope) string { return s.Owner }, f.owner),
		},
		Context:    []searchexec.ContextScope{searchexec.ScopeFunc(seWorkspace, f.workspace)},
		Conditions: []BoolExpression{f.deleted.IS_FALSE()},
		Keys:       f.keys,
		Tiebreak:   f.tiebreak,
		Default:    searchexec.Desc(seRank),
		Limit:      20,
	}
}

func (f *seFixture) search(s seScope) seItemSearch {
	return searchexec.New[seItemSearch](f.itemSpec, s)
}

var seScopeA = seScope{Org: "org-a", Team: "t1", Owner: "u1"}

// match is the item search's filter in Go: the rows the database must return for scope s
// and workspace w ("" for every workspace).
func (f *seFixture) match(s seScope, w string) []seRow {
	var rows []seRow
	for _, r := range f.rows {
		if r.Org == s.Org && seTeams[r.Team] == s.Org && r.Team == s.Team &&
			(r.Owner == nil || *r.Owner == s.Owner) && (w == "" || r.Space == w) && !r.Gone {
			rows = append(rows, r)
		}
	}
	return rows
}

// seCompare orders two rows as the keyset order of sorts does, in Go: each sort with its NULL
// placement, then the id in the direction of the last sort.
func seCompare(sorts []search.Sort[seSort]) func(a, b seRow) int {
	return func(a, b seRow) int {
		last := search.SortAscending
		for _, s := range sorts {
			last = s.Direction
			desc := s.Direction == search.SortDescending
			var c int
			switch s.Field {
			case seRank:
				c = cmp.Compare(a.Rank, b.Rank)
				if desc {
					c = -c
				}
			case seScore, seScoreNF, seScoreNL:
				nullsLast := !desc
				if s.Field == seScoreNF {
					nullsLast = false
				} else if s.Field == seScoreNL {
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
	}
}

func compareNullable(a, b *int64, desc, nullsLast bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		if nullsLast {
			return 1
		}
		return -1
	case b == nil:
		if nullsLast {
			return -1
		}
		return 1
	case desc:
		return cmp.Compare(*b, *a)
	default:
		return cmp.Compare(*a, *b)
	}
}

func seIDs(rows []seRow) []int64 {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func TestSearchexecWalkMatchesGoOrder(t *testing.T) {
	f := newSeFixture(t)
	orderings := [][]search.Sort[seSort]{
		nil, // the default sort: rank descending
		{searchexec.Asc(seRank)},
		{searchexec.Asc(seScore)},
		{searchexec.Desc(seScore)},
		{searchexec.Asc(seScoreNF)},
		{searchexec.Desc(seScoreNL)},
		{searchexec.Desc(seRank), searchexec.Asc(seScore)},
		{searchexec.Asc(seScoreNL), searchexec.Desc(seRank)},
	}
	for _, sorts := range orderings {
		t.Run(fmt.Sprint(sorts), func(t *testing.T) {
			if sourceIsCockroachDB() && slices.ContainsFunc(sorts, func(s search.Sort[seSort]) bool {
				return s.Field == seScore
			}) {
				// The keyset pager writes the PostgreSQL default NULL placement (last when
				// ascending), and CockroachDB sorts NULLs first. The keyset suite skips
				// CockroachDB for the same reason, and ADR 0015 drops it.
				t.Skip("CockroachDB places NULLs first in ascending order")
			}
			want := f.match(seScopeA, "")
			require.Greater(t, len(want), 14, "the walk spans several pages")
			goSorts := sorts
			if goSorts == nil {
				goSorts = []search.Sort[seSort]{searchexec.Desc(seRank)}
			}
			slices.SortFunc(want, seCompare(goSorts))

			q := f.search(seScopeA).Limit(7)
			for _, s := range sorts {
				q = q.SortBy(s.Field, s.Direction)
			}
			var got []seRow
			for pages := 1; ; pages++ {
				require.LessOrEqual(t, pages, len(want), "the walk does not end")
				result, err := searchexec.Run(t.Context(), db, f.itemSpec, q.Query, func(r seRow) seRow { return r })
				require.NoError(t, err)
				require.Equal(t, int64(len(want)), result.Total)
				require.Equal(t, len(result.Items), result.Count)
				got = append(got, result.Items...)
				if !result.HasMore() {
					break
				}
				require.Len(t, result.Items, 7)
				q = q.After(result.NextCursor)
			}
			require.Equal(t, seIDs(want), seIDs(got))
		})
	}
}

func TestSearchexecScopeOrNullAndScopeNull(t *testing.T) {
	f := newSeFixture(t)

	t.Run("ScopeOrNull keeps the owner's rows and the rows with no owner", func(t *testing.T) {
		result, err := searchexec.Run(t.Context(), db, f.itemSpec, f.search(seScopeA).Limit(search.MaxLimit).Query,
			func(r seRow) seRow { return r })
		require.NoError(t, err)
		var owned, shared int
		for _, r := range result.Items {
			if r.Owner == nil {
				shared++
			} else {
				require.Equal(t, "u1", *r.Owner)
				owned++
			}
		}
		require.Positive(t, owned)
		require.Positive(t, shared)
		require.ElementsMatch(t, seIDs(f.match(seScopeA, "")), seIDs(result.Items))
	})

	t.Run("ScopeNull keeps the rows with no owner only", func(t *testing.T) {
		var want []int64
		for _, r := range f.rows {
			if r.Org == "org-a" && r.Owner == nil {
				want = append(want, r.ID)
			}
		}
		q := searchexec.New[seOrgSearch](f.orgSpec, "org-a").Limit(search.MaxLimit)
		result, err := searchexec.Run(t.Context(), db, f.orgSpec, q.Query, func(r seRow) seRow { return r })
		require.NoError(t, err)
		require.NotEmpty(t, want)
		require.ElementsMatch(t, want, seIDs(result.Items))
	})
}

func TestSearchexecContextScope(t *testing.T) {
	f := newSeFixture(t)
	q := f.search(seScopeA).Limit(search.MaxLimit)

	ctx := context.WithValue(t.Context(), seWorkspaceKey{}, "w2")
	result, err := searchexec.Run(ctx, db, f.itemSpec, q.Query, func(r seRow) seRow { return r })
	require.NoError(t, err)
	require.NotEmpty(t, result.Items)
	require.ElementsMatch(t, seIDs(f.match(seScopeA, "w2")), seIDs(result.Items))

	ctx = context.WithValue(t.Context(), seWorkspaceKey{}, "nil")
	_, err = searchexec.Run(ctx, db, f.itemSpec, q.Query, func(r seRow) seRow { return r })
	require.ErrorIs(t, err, search.ErrUnscoped)
}

func TestSearchexecPlanAndNarrows(t *testing.T) {
	f := newSeFixture(t)
	plan, err := searchexec.Resolve(t.Context(), f.itemSpec, f.search(seScopeA).Limit(search.MaxLimit).Query)
	require.NoError(t, err)
	order, err := plan.Order(nil)
	require.NoError(t, err)

	// The extra condition holds an OR that would read rows of org-b if it widened the scope.
	narrowed := plan.And(f.org.EQ(String("org-b")).OR(f.rank.EQ(Int(1))))
	result, err := narrowed.Run(t.Context(), db, order, func(r seRow) seRow { return r })
	require.NoError(t, err)

	var want []int64
	for _, r := range f.match(seScopeA, "") {
		if r.Rank == 1 {
			want = append(want, r.ID)
		}
	}
	require.NotEmpty(t, want)
	require.ElementsMatch(t, want, seIDs(result.Items))
	require.Equal(t, int64(len(want)), result.Total)
}

func TestSearchexecOffset(t *testing.T) {
	f := newSeFixture(t)
	want := f.match(seScopeA, "")
	slices.SortFunc(want, seCompare([]search.Sort[seSort]{searchexec.Desc(seRank)}))
	require.Greater(t, len(want), 13)
	run := func(q seItemSearch) search.Result[seRow] {
		t.Helper()
		result, err := searchexec.Run(t.Context(), db, f.itemSpec, q.Query, func(r seRow) seRow { return r })
		require.NoError(t, err)
		require.Equal(t, int64(len(want)), result.Total)
		return result
	}

	q := f.search(seScopeA).Limit(4)
	page := run(q.Offset(5))
	require.Equal(t, seIDs(want[5:9]), seIDs(page.Items))

	// The cursor of an offset page continues after it.
	next := run(q.After(page.NextCursor))
	require.Equal(t, seIDs(want[9:13]), seIDs(next.Items))

	// Offset clears the cursor: the page starts at the offset, not after the cursor.
	again := run(q.After(page.NextCursor).Offset(2))
	require.Equal(t, seIDs(want[2:6]), seIDs(again.Items))
}

func TestSearchexecEmptyResult(t *testing.T) {
	f := newSeFixture(t)
	result, err := searchexec.Run(t.Context(), db, f.itemSpec,
		f.search(seScope{Org: "org-z", Team: "t1", Owner: "u1"}).Query, func(r seRow) seRow { return r })
	require.NoError(t, err)
	require.Equal(t, search.Result[seRow]{Items: []seRow{}}, result)
}

func TestSearchexecRunPage(t *testing.T) {
	f := newSeFixture(t)
	want := f.match(seScopeA, "")
	slices.SortFunc(want, seCompare([]search.Sort[seSort]{searchexec.Desc(seRank)}))

	var calls int
	result, err := searchexec.RunPage(t.Context(), db, f.itemSpec, f.search(seScopeA).Limit(6).Query,
		func(_ context.Context, rows []seRow) ([]int64, error) {
			calls++
			return seIDs(rows), nil
		})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, seIDs(want[:6]), result.Items, "the mapper gets the page's rows only")
	require.Equal(t, 6, result.Count)
	require.True(t, result.HasMore())
}

func TestSearchexecRefusesBeforeAnyStatement(t *testing.T) {
	f := newSeFixture(t)
	other := searchexec.DefineSpec(f.itemDef())
	cases := map[string]struct {
		spec *searchexec.Spec[seRow, seSort, seScope]
		q    seItemSearch
		want error
	}{
		"zero scope field":      {f.itemSpec, f.search(seScope{Org: "org-a", Owner: "u1"}), search.ErrUnscoped},
		"struct literal":        {f.itemSpec, seItemSearch{}, search.ErrUnscoped},
		"query of another spec": {other, f.search(seScopeA), search.ErrUnscoped},
		"limit 0":               {f.itemSpec, f.search(seScopeA).Limit(0), search.ErrInvalidLimit},
		"limit above the max":   {f.itemSpec, f.search(seScopeA).Limit(search.MaxLimit + 1), search.ErrInvalidLimit},
		"negative offset":       {f.itemSpec, f.search(seScopeA).Offset(-1), search.ErrInvalidOffset},
		"cursor of another sort": {
			f.itemSpec, f.search(seScopeA).SortBy(seScore, search.SortAscending).After(f.cursor(t)),
			search.ErrInvalidCursor,
		},
		"unknown sort": {f.itemSpec, f.search(seScopeA).SortBy("name", search.SortAscending), search.ErrUnknownSort},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := searchexec.Run(t.Context(), db, tc.spec, tc.q.Query, func(r seRow) seRow { return r })
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// cursor returns the next cursor of the first page in the default order.
func (f *seFixture) cursor(t *testing.T) string {
	t.Helper()
	result, err := searchexec.Run(t.Context(), db, f.itemSpec, f.search(seScopeA).Limit(2).Query,
		func(r seRow) seRow { return r })
	require.NoError(t, err)
	require.NotEmpty(t, result.NextCursor)
	return result.NextCursor
}

// TestSearchexecHandWrittenSort runs a sort that the spec does not declare, as a hand-written
// repository method does: Resolve, Plan.Order with a computed key, Plan.Run. The page selects
// the computed key, so the row and the next cursor carry its value.
func TestSearchexecHandWrittenSort(t *testing.T) {
	f := newSeFixture(t)
	weight := keyset.IntExprKey("se_row.weight", f.rank.MUL(Int(10)).ADD(f.id.MOD(Int(7))),
		func(r seRow) int64 { return r.Weight })
	want := f.match(seScopeA, "")
	slices.SortFunc(want, func(a, b seRow) int {
		wa, wb := a.Rank*10+a.ID%7, b.Rank*10+b.ID%7
		return cmp.Or(cmp.Compare(wb, wa), cmp.Compare(b.ID, a.ID))
	})

	q := f.search(seScopeA).SortBy("weight", search.SortDescending).Limit(6)
	_, err := searchexec.Run(t.Context(), db, f.itemSpec, q.Query, func(r seRow) seRow { return r })
	require.ErrorIs(t, err, search.ErrUnknownSort, "Run knows the declared keys only")

	var got []seRow
	for pages := 1; ; pages++ {
		require.LessOrEqual(t, pages, len(want), "the walk does not end")
		plan, resolveErr := searchexec.Resolve(t.Context(), f.itemSpec, q.Query)
		require.NoError(t, resolveErr)
		order, orderErr := plan.Order(map[seSort]keyset.Key[seRow]{"weight": weight})
		require.NoError(t, orderErr)
		result, runErr := plan.Run(t.Context(), db, order, func(r seRow) seRow { return r })
		require.NoError(t, runErr)
		for _, r := range result.Items {
			require.Equal(t, r.Rank*10+r.ID%7, r.Weight)
		}
		got = append(got, result.Items...)
		if !result.HasMore() {
			break
		}
		q = q.After(result.NextCursor)
	}
	require.Equal(t, seIDs(want), seIDs(got))
}
