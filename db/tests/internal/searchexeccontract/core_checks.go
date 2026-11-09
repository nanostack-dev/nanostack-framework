// This file is Nanostack-only (not in go-jet): the checks of the db/searchexec core (R6) on
// every dialect. See db/UPSTREAM.md.

package searchexeccontract

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

func (s suite) runCore(t *testing.T) {
	f := s.newSeFixture(t)
	t.Run("a walk matches the order computed in Go", func(t *testing.T) { s.coreWalk(t, f) })
	t.Run("ScopeOrNull and ScopeNull", func(t *testing.T) { s.coreScopeOrNull(t, f) })
	t.Run("a context scope", func(t *testing.T) { s.coreContextScope(t, f) })
	t.Run("Plan.And narrows", func(t *testing.T) { s.corePlanAnd(t, f) })
	t.Run("Offset", func(t *testing.T) { s.coreOffset(t, f) })
	t.Run("an empty result", func(t *testing.T) { s.coreEmpty(t, f) })
	t.Run("RunPage", func(t *testing.T) { s.coreRunPage(t, f) })
	t.Run("refusals run no statement", func(t *testing.T) { s.coreRefusals(t, f) })
	t.Run("a hand-written computed sort", func(t *testing.T) { s.coreHandWrittenSort(t, f) })
}

func (s suite) coreWalk(t *testing.T, f *seFixture) {
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
			s.skipNullOrder(t, sorts)
			want := f.match(seScopeA, "")
			require.Greater(t, len(want), 14, "the walk spans several pages")
			goSorts := sorts
			if goSorts == nil {
				goSorts = []search.Sort[seSort]{searchexec.Desc(seRank)}
			}
			slices.SortFunc(want, seCompare(goSorts))

			q := f.search(seScopeA).Limit(7)
			for _, sort := range sorts {
				q = q.SortBy(sort.Field, sort.Direction)
			}
			var got []seRow
			for pages := 1; ; pages++ {
				require.LessOrEqual(t, pages, len(want), "the walk does not end")
				result, err := searchexec.Run(t.Context(), s.cfg.DB, f.itemSpec, q.Query, seIdentity)
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

func (s suite) coreScopeOrNull(t *testing.T, f *seFixture) {
	t.Run("ScopeOrNull keeps the owner's rows and the rows with no owner", func(t *testing.T) {
		result, err := searchexec.Run(t.Context(), s.cfg.DB, f.itemSpec,
			f.search(seScopeA).Limit(search.MaxLimit).Query, seIdentity)
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
		result, err := searchexec.Run(t.Context(), s.cfg.DB, f.orgSpec, q.Query, seIdentity)
		require.NoError(t, err)
		require.NotEmpty(t, want)
		require.ElementsMatch(t, want, seIDs(result.Items))
	})
}

func (s suite) coreContextScope(t *testing.T, f *seFixture) {
	q := f.search(seScopeA).Limit(search.MaxLimit)

	ctx := context.WithValue(t.Context(), seWorkspaceKey{}, "w2")
	result, err := searchexec.Run(ctx, s.cfg.DB, f.itemSpec, q.Query, seIdentity)
	require.NoError(t, err)
	require.NotEmpty(t, result.Items)
	require.ElementsMatch(t, seIDs(f.match(seScopeA, "w2")), seIDs(result.Items))

	ctx = context.WithValue(t.Context(), seWorkspaceKey{}, "nil")
	_, err = searchexec.Run(ctx, s.cfg.DB, f.itemSpec, q.Query, seIdentity)
	require.ErrorIs(t, err, search.ErrUnscoped)
}

func (s suite) corePlanAnd(t *testing.T, f *seFixture) {
	plan, err := searchexec.Resolve(t.Context(), f.itemSpec, f.search(seScopeA).Limit(search.MaxLimit).Query)
	require.NoError(t, err)
	order, err := plan.Order(nil)
	require.NoError(t, err)

	// The extra condition holds an OR that would read rows of org-b if it widened the scope.
	narrowed := plan.And(f.org.EQ(searchexec.StringLit("org-b")).OR(f.rank.EQ(Int(1))))
	result, err := narrowed.Run(t.Context(), s.cfg.DB, order, seIdentity)
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

func (s suite) coreOffset(t *testing.T, f *seFixture) {
	want := f.match(seScopeA, "")
	slices.SortFunc(want, seCompare([]search.Sort[seSort]{searchexec.Desc(seRank)}))
	require.Greater(t, len(want), 13)
	run := func(q seItemSearch) search.Result[seRow] {
		t.Helper()
		result, err := searchexec.Run(t.Context(), s.cfg.DB, f.itemSpec, q.Query, seIdentity)
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

func (s suite) coreEmpty(t *testing.T, f *seFixture) {
	result, err := searchexec.Run(t.Context(), s.cfg.DB, f.itemSpec,
		f.search(seScope{Org: "org-z", Team: "t1", Owner: "u1"}).Query, seIdentity)
	require.NoError(t, err)
	require.Equal(t, search.Result[seRow]{Items: []seRow{}}, result)
}

func (s suite) coreRunPage(t *testing.T, f *seFixture) {
	want := f.match(seScopeA, "")
	slices.SortFunc(want, seCompare([]search.Sort[seSort]{searchexec.Desc(seRank)}))

	var calls int
	result, err := searchexec.RunPage(t.Context(), s.cfg.DB, f.itemSpec, f.search(seScopeA).Limit(6).Query,
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

func (s suite) coreRefusals(t *testing.T, f *seFixture) {
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
			f.itemSpec, f.search(seScopeA).SortBy(seScore, search.SortAscending).After(s.firstCursor(t, f)),
			search.ErrInvalidCursor,
		},
		"unknown sort": {f.itemSpec, f.search(seScopeA).SortBy("name", search.SortAscending), search.ErrUnknownSort},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := searchexec.Run(t.Context(), s.cfg.DB, tc.spec, tc.q.Query, seIdentity)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// firstCursor returns the next cursor of the first page in the default order.
func (s suite) firstCursor(t *testing.T, f *seFixture) string {
	t.Helper()
	result, err := searchexec.Run(t.Context(), s.cfg.DB, f.itemSpec, f.search(seScopeA).Limit(2).Query, seIdentity)
	require.NoError(t, err)
	require.NotEmpty(t, result.NextCursor)
	return result.NextCursor
}

// coreHandWrittenSort runs a sort that the spec does not declare, as a hand-written
// repository method does: Resolve, Plan.Order with a computed key, Plan.Run. The page
// selects the computed key, so the row and the next cursor carry its value.
func (s suite) coreHandWrittenSort(t *testing.T, f *seFixture) {
	weight := keyset.IntExprKey("se_row.weight", f.rank.MUL(Int(10)).ADD(f.id.MOD(Int(7))),
		func(r seRow) int64 { return r.Weight })
	want := f.match(seScopeA, "")
	slices.SortFunc(want, func(a, b seRow) int {
		wa, wb := a.Rank*10+a.ID%7, b.Rank*10+b.ID%7
		return cmp.Or(cmp.Compare(wb, wa), cmp.Compare(b.ID, a.ID))
	})

	q := f.search(seScopeA).SortBy("weight", search.SortDescending).Limit(6)
	_, err := searchexec.Run(t.Context(), s.cfg.DB, f.itemSpec, q.Query, seIdentity)
	require.ErrorIs(t, err, search.ErrUnknownSort, "Run knows the declared keys only")

	var got []seRow
	for pages := 1; ; pages++ {
		require.LessOrEqual(t, pages, len(want), "the walk does not end")
		plan, resolveErr := searchexec.Resolve(t.Context(), f.itemSpec, q.Query)
		require.NoError(t, resolveErr)
		order, orderErr := plan.Order(map[seSort]keyset.Key[seRow]{"weight": weight})
		require.NoError(t, orderErr)
		result, runErr := plan.Run(t.Context(), s.cfg.DB, order, seIdentity)
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
