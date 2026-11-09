package searchexec_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// The fixture is a search of items joined to their team, written by hand as nano gen repo
// would write it: the organization is scoped on both tables through one field, the team on
// items, the owner with ScopeOrNull, and the workspace through a context scope.

type item struct {
	ID    string `alias:"items.id"`
	Rank  int64  `alias:"items.rank"`
	Owner *string
}

type itemSort string

const (
	itemSortRank itemSort = "rank"
	itemSortName itemSort = "name"
)

type itemScope struct {
	_     struct{}
	Org   string
	Team  string
	Owner string
}

type itemSearch struct {
	searchexec.Query[item, itemSearch, itemSort, itemScope]
}

var (
	itemID        = postgres.StringColumn("id")
	itemOrg       = postgres.StringColumn("org")
	itemTeam      = postgres.StringColumn("team_id")
	itemOwner     = postgres.StringColumn("owner")
	itemRank      = postgres.IntegerColumn("rank")
	itemName      = postgres.StringColumn("name")
	itemWorkspace = postgres.StringColumn("workspace")
	itemDeleted   = postgres.BoolColumn("deleted")
	items         = postgres.NewTable("public", "items", "",
		itemID, itemOrg, itemTeam, itemOwner, itemRank, itemName, itemWorkspace, itemDeleted)

	teamID  = postgres.StringColumn("id")
	teamOrg = postgres.StringColumn("org")
	teams   = postgres.NewTable("public", "teams", "", teamID, teamOrg)
)

type workspaceKey struct{}

// workspaceFilter is the context scope: the workspace of the context, TRUE when the context
// has none, and nil when the context says so (a broken scope function).
func workspaceFilter(ctx context.Context, column postgres.ColumnString) postgres.BoolExpression {
	switch v, _ := ctx.Value(workspaceKey{}).(string); v {
	case "":
		return postgres.Bool(true)
	case "nil":
		return nil
	default:
		return column.EQ(postgres.String(v))
	}
}

func itemDef() searchexec.SpecDef[item, itemSort, itemScope] {
	return searchexec.SpecDef[item, itemSort, itemScope]{
		From: func(itemScope) keyset.Source {
			return items.INNER_JOIN(teams, teamID.EQ(itemTeam))
		},
		Columns: postgres.ProjectionList{itemID, itemRank, itemOwner},
		Scope: []searchexec.ScopeCol[itemScope]{
			searchexec.ScopeEq(func(s itemScope) string { return s.Org }, itemOrg, teamOrg),
			searchexec.ScopeEq(func(s itemScope) string { return s.Team }, itemTeam),
			searchexec.ScopeOrNull(func(s itemScope) string { return s.Owner }, itemOwner),
		},
		Context:    []searchexec.ContextScope{searchexec.ScopeFunc(workspaceFilter, itemWorkspace)},
		Unscoped:   []string{"items.region: the region is public"},
		Conditions: []postgres.BoolExpression{itemDeleted.IS_FALSE()},
		Keys: map[itemSort]keyset.Key[item]{
			itemSortRank: keyset.IntKey(itemRank, func(r item) int64 { return r.Rank }),
		},
		Tiebreak: []keyset.Key[item]{keyset.StringKey(itemID, func(r item) string { return r.ID })},
		Default:  searchexec.Desc(itemSortRank),
		Limit:    20,
	}
}

var itemSpec = searchexec.DefineSpec(itemDef())

func newItemSearch(s itemScope) itemSearch { return searchexec.New[itemSearch](itemSpec, s) }

func fullScope() itemScope { return itemScope{Org: "org-a", Team: "team-1", Owner: "user-1"} }

// recorder is an executor that records each statement and answers it with errStop, so a
// test sees the SQL of a run without a database.
type recorder struct {
	queries []string
	args    [][]any
}

var errStop = errors.New("recorder: stop")

func (r *recorder) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	r.queries = append(r.queries, query)
	r.args = append(r.args, args)
	return nil, errStop
}

func identity(r item) item { return r }

// countSQL runs q and returns the count statement, the first statement of a run.
func countSQL(t *testing.T, ctx context.Context, q itemSearch) (string, []any) {
	t.Helper()
	rec := &recorder{}
	_, err := searchexec.Run(ctx, rec, itemSpec, q.Query, identity)
	require.ErrorIs(t, err, errStop)
	require.Len(t, rec.queries, 1)
	return rec.queries[0], rec.args[0]
}

func TestRunWritesEveryScope(t *testing.T) {
	ctx := context.WithValue(t.Context(), workspaceKey{}, "ws-1")
	query, args := countSQL(t, ctx, newItemSearch(fullScope()))

	assert.Equal(t, "SELECT COUNT(*) FROM public.items INNER JOIN public.teams ON (teams.id = items.team_id) "+
		"WHERE ( ( (items.org = $1) AND (teams.org = $2) ) AND (items.team_id = $3) "+
		"AND ( (items.owner = $4) OR (items.owner IS NULL) ) AND (items.workspace = $5::text) "+
		"AND (items.deleted IS FALSE) );", flat(query))
	assert.Equal(t, []any{"org-a", "org-a", "team-1", "user-1", "ws-1"}, args)
}

func TestContextScopeMayReturnTrue(t *testing.T) {
	query, args := countSQL(t, t.Context(), newItemSearch(fullScope()))
	assert.Contains(t, flat(query), "AND $5::boolean AND (items.deleted IS FALSE)")
	assert.Equal(t, true, args[4])
}

func TestResolveRefusesAnUnscopedQuery(t *testing.T) {
	other := searchexec.DefineSpec(itemDef())
	cases := map[string]struct {
		ctx  context.Context
		spec *searchexec.Spec[item, itemSort, itemScope]
		q    itemSearch
		want string
	}{
		"zero field read by two columns": {
			spec: itemSpec,
			q:    newItemSearch(itemScope{Team: "team-1", Owner: "user-1"}),
			want: "items.org, teams.org",
		},
		"zero field": {
			spec: itemSpec,
			q:    newItemSearch(itemScope{Org: "org-a", Owner: "user-1"}),
			want: "items.team_id",
		},
		"zero ScopeOrNull field": {
			spec: itemSpec,
			q:    newItemSearch(itemScope{Org: "org-a", Team: "team-1"}),
			want: "items.owner",
		},
		"struct literal of the builder": {spec: itemSpec, q: itemSearch{}, want: "not built from"},
		"struct literal changed by its methods": {
			spec: itemSpec,
			q:    itemSearch{}.SortBy(itemSortRank, search.SortAscending).Limit(5),
			want: "not built from",
		},
		"query of another spec": {spec: other, q: newItemSearch(fullScope()), want: "not built from"},
		"nil spec":              {spec: nil, q: newItemSearch(fullScope()), want: "no spec"},
		"nil context scope": {
			ctx:  context.WithValue(t.Context(), workspaceKey{}, "nil"),
			spec: itemSpec,
			q:    newItemSearch(fullScope()),
			want: "context scope 0 returned no condition",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := tc.ctx
			if ctx == nil {
				ctx = t.Context()
			}
			_, err := searchexec.Resolve(ctx, tc.spec, tc.q.Query)
			require.ErrorIs(t, err, search.ErrUnscoped)
			assert.False(t, search.IsClientError(err))
			assert.Contains(t, err.Error(), tc.want)

			rec := &recorder{}
			_, err = searchexec.Run(ctx, rec, tc.spec, tc.q.Query, identity)
			require.ErrorIs(t, err, search.ErrUnscoped)
			_, err = searchexec.RunPage(ctx, rec, tc.spec, tc.q.Query,
				func(context.Context, []item) ([]item, error) { return nil, nil })
			require.ErrorIs(t, err, search.ErrUnscoped)
			assert.Empty(t, rec.queries, "no statement runs")
		})
	}
}

func TestZeroScopeErrorDoesNotHoldTheValue(t *testing.T) {
	_, err := searchexec.Resolve(t.Context(), itemSpec,
		newItemSearch(itemScope{Org: "secret-org", Owner: "secret-user"}).Query)
	require.ErrorIs(t, err, search.ErrUnscoped)
	assert.NotContains(t, err.Error(), "secret")
}

func TestResolveChecksThePage(t *testing.T) {
	q := newItemSearch(fullScope())
	cases := map[string]struct {
		q    itemSearch
		want error
	}{
		"limit 0":             {q.Limit(0), search.ErrInvalidLimit},
		"limit above the max": {q.Limit(search.MaxLimit + 1), search.ErrInvalidLimit},
		"negative offset":     {q.Offset(-1), search.ErrInvalidOffset},
		"cursor too long":     {q.After(strings.Repeat("a", search.MaxCursorLength+1)), search.ErrInvalidCursor},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{}
			_, err := searchexec.Run(t.Context(), rec, itemSpec, tc.q.Query, identity)
			require.ErrorIs(t, err, tc.want)
			assert.True(t, search.IsClientError(err))
			assert.Empty(t, rec.queries, "no statement runs")
		})
	}
}

func TestPageSetters(t *testing.T) {
	q := newItemSearch(fullScope())
	page := func(q itemSearch) search.Pagination {
		t.Helper()
		plan, err := searchexec.Resolve(t.Context(), itemSpec, q.Query)
		require.NoError(t, err)
		return plan.Page()
	}

	assert.Equal(t, search.Pagination{Limit: 20}, page(q), "the default limit")
	assert.Equal(t, search.Pagination{Limit: 5}, page(q.Limit(5)))
	assert.Equal(t, search.Pagination{Limit: 20, Offset: 3}, page(q.After("token").Offset(3)), "Offset clears a cursor")
	assert.Equal(t, search.Pagination{Limit: 20, Cursor: "token"}, page(q.Offset(3).After("token")),
		"After clears an offset")
	assert.Equal(t, search.Pagination{Limit: 20, Offset: 3}, page(q.Offset(3).After("")),
		"an empty cursor keeps the offset")
	assert.Equal(t, search.Pagination{Limit: 20, Cursor: "token"}, page(q.After("token").Offset(0)),
		"a zero offset keeps the cursor")
}

func TestQueryIsImmutable(t *testing.T) {
	sorts := func(q itemSearch) []search.Sort[itemSort] {
		t.Helper()
		plan, err := searchexec.Resolve(t.Context(), itemSpec, q.Query)
		require.NoError(t, err)
		return plan.Sorts()
	}
	base := newItemSearch(fullScope())
	byRank := base.SortBy(itemSortRank, search.SortAscending)
	// Two searches built from byRank must not share the backing array of its sorts.
	thenName := byRank.SortBy(itemSortName, search.SortAscending)
	thenRank := byRank.SortBy(itemSortRank, search.SortDescending)
	limited := byRank.Limit(3)

	assert.Equal(t, []search.Sort[itemSort]{searchexec.Desc(itemSortRank)}, sorts(base), "the default sort")
	assert.Equal(t, []search.Sort[itemSort]{searchexec.Asc(itemSortRank)}, sorts(byRank))
	assert.Equal(t, []search.Sort[itemSort]{searchexec.Asc(itemSortRank), searchexec.Asc(itemSortName)},
		sorts(thenName))
	assert.Equal(t, []search.Sort[itemSort]{searchexec.Asc(itemSortRank), searchexec.Desc(itemSortRank)},
		sorts(thenRank))
	assert.Equal(t, fullScope(), limited.Scope())

	// Three sorts leave spare capacity in the backing array (append grows 2 to 4), so two
	// searches built from the same three-sort search would overwrite each other's fourth sort
	// if SortBy appended in place.
	three := thenName.SortBy(itemSortName, search.SortDescending)
	fourAsc := three.SortBy(itemSortRank, search.SortAscending)
	fourDesc := three.SortBy(itemSortRank, search.SortDescending)
	assert.Equal(t, searchexec.Asc(itemSortRank), sorts(fourAsc)[3])
	assert.Equal(t, searchexec.Desc(itemSortRank), sorts(fourDesc)[3])
	assert.Len(t, sorts(three), 3)

	plan, err := searchexec.Resolve(t.Context(), itemSpec, byRank.Query)
	require.NoError(t, err)
	plan.Sorts()[0].Field = itemSortName
	assert.Equal(t, []search.Sort[itemSort]{searchexec.Asc(itemSortRank)}, plan.Sorts(), "Sorts returns a copy")
	assert.Equal(t, int32(20), plan.Page().Limit, "Limit on a copy leaves the original")
}

func TestPlanAndNarrows(t *testing.T) {
	plan, err := searchexec.Resolve(t.Context(), itemSpec, newItemSearch(fullScope()).Query)
	require.NoError(t, err)

	run := func(plan searchexec.Plan[item, itemSort]) string {
		t.Helper()
		order, orderErr := plan.Order(nil)
		require.NoError(t, orderErr)
		rec := &recorder{}
		_, runErr := plan.Run(t.Context(), rec, order, identity)
		require.ErrorIs(t, runErr, errStop)
		require.Len(t, rec.queries, 1)
		return rec.queries[0]
	}
	before := run(plan)
	assert.Equal(t, before, run(plan.And(nil)), "nil adds nothing")

	// The plan's condition and the extra condition are the two terms of one AND, and an OR
	// in the extra condition stays inside its own parentheses.
	where := func(query string) string {
		return flat(query[strings.Index(query, "WHERE ")+len("WHERE ") : strings.LastIndex(query, ";")])
	}
	narrowed := run(plan.And(itemOrg.EQ(postgres.String("org-b")).OR(itemRank.EQ(postgres.Int(1)))))
	assert.Equal(t, "( "+where(before)+" AND ((items.org = $6::text) OR (items.rank = $7)) )", where(narrowed))
	assert.Equal(t, before, run(plan), "And returns a copy")
}

// flat collapses the white space of a statement, so a test reads its SQL on one line.
func flat(query string) string {
	return strings.Join(strings.Fields(query), " ")
}

func TestPlanOrder(t *testing.T) {
	computed := keyset.IntKey(postgres.IntegerColumn("score"), func(item) int64 { return 0 })
	q := newItemSearch(fullScope()).SortBy("score", search.SortDescending)
	plan, err := searchexec.Resolve(t.Context(), itemSpec, q.Query)
	require.NoError(t, err)

	_, err = plan.Order(nil)
	require.ErrorIs(t, err, search.ErrUnknownSort, "a hand-written sort needs its key")
	rec := &recorder{}
	_, err = searchexec.Run(t.Context(), rec, itemSpec, q.Query, identity)
	require.ErrorIs(t, err, search.ErrUnknownSort)
	assert.Empty(t, rec.queries)

	order, err := plan.Order(map[itemSort]keyset.Key[item]{"score": computed})
	require.NoError(t, err)
	assert.Equal(t, "score:DESC,items.id:DESC", order.Signature())

	_, err = plan.Order(map[itemSort]keyset.Key[item]{itemSortRank: computed})
	require.Error(t, err, "a hand-written key cannot replace a declared sort")
	assert.Contains(t, err.Error(), `"rank"`)

	// Order copies the declared keys before it adds the hand-written ones: the spec's map is
	// shared by every request, so the extra key must not stay in it.
	_, err = plan.Order(map[itemSort]keyset.Key[item]{"score": computed})
	require.NoError(t, err, "the same extra key a second time")
	_, err = plan.Order(nil)
	require.ErrorIs(t, err, search.ErrUnknownSort, "the extra key did not go into the plan")
	_, err = searchexec.Run(t.Context(), rec, itemSpec, q.Query, identity)
	require.ErrorIs(t, err, search.ErrUnknownSort, "the extra key did not go into the spec")
	assert.Empty(t, rec.queries)
}

func TestZeroPlanDoesNotRun(t *testing.T) {
	var plan searchexec.Plan[item, itemSort]
	rec := &recorder{}

	_, err := plan.Order(nil)
	require.ErrorIs(t, err, search.ErrUnscoped)
	_, err = plan.Run(t.Context(), rec, keyset.Order[item]{}, identity)
	require.ErrorIs(t, err, search.ErrUnscoped)
	_, err = plan.RunPage(t.Context(), rec, keyset.Order[item]{},
		func(context.Context, []item) ([]item, error) { return nil, nil })
	require.ErrorIs(t, err, search.ErrUnscoped)
	_, err = plan.And(postgres.Bool(true)).Run(t.Context(), rec, keyset.Order[item]{}, identity)
	require.ErrorIs(t, err, search.ErrUnscoped, "And does not make a zero plan run")
	assert.Empty(t, rec.queries)
}

func TestResolveRefusesASourceOfAnotherDialect(t *testing.T) {
	sources := map[string]keyset.Source{
		"mysql":  mysql.NewTable("db", "items", "", mysql.StringColumn("id")),
		"sqlite": sqlite.NewTable("", "items", "", sqlite.StringColumn("id")),
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			def := itemDef()
			def.From = func(itemScope) keyset.Source { return source }
			spec := searchexec.DefineSpec(def)
			_, err := searchexec.Resolve(t.Context(), spec, searchexec.New[itemSearch](spec, fullScope()).Query)
			require.ErrorIs(t, err, keyset.ErrDialect)
		})
	}

	def := itemDef()
	def.From = func(itemScope) keyset.Source { return nil }
	spec := searchexec.DefineSpec(def)
	_, err := searchexec.Resolve(t.Context(), spec, searchexec.New[itemSearch](spec, fullScope()).Query)
	require.ErrorIs(t, err, keyset.ErrPageSourceMissing)
}

func TestFromGetsTheScope(t *testing.T) {
	def := itemDef()
	var got itemScope
	def.From = func(s itemScope) keyset.Source {
		got = s
		return items
	}
	spec := searchexec.DefineSpec(def)
	_, err := searchexec.Resolve(t.Context(), spec, searchexec.New[itemSearch](spec, fullScope()).Query)
	require.NoError(t, err)
	assert.Equal(t, fullScope(), got)
}

func TestDefineSpecCopiesTheDefinition(t *testing.T) {
	def := itemDef()
	spec := searchexec.DefineSpec(def)
	def.Scope[0] = searchexec.ScopeNull[itemScope](itemOrg)
	def.Conditions[0] = postgres.Bool(true)
	delete(def.Keys, itemSortRank)
	def.Tiebreak[0] = keyset.IntKey(itemRank, func(r item) int64 { return r.Rank })
	def.Context[0] = func(context.Context) postgres.BoolExpression { return postgres.Bool(true) }

	ctx := context.WithValue(t.Context(), workspaceKey{}, "ws-1")
	q := searchexec.New[itemSearch](spec, fullScope())
	rec := &recorder{}
	_, err := searchexec.Run(ctx, rec, spec, q.Query, identity)
	require.ErrorIs(t, err, errStop)
	assert.Contains(t, rec.queries[0], "(items.org = $1)")
	assert.Contains(t, rec.queries[0], "items.deleted IS FALSE")
	assert.Contains(t, rec.queries[0], "items.workspace = ", "the context scope is the copy")

	plan, err := searchexec.Resolve(ctx, spec, q.Query)
	require.NoError(t, err)
	order, err := plan.Order(nil)
	require.NoError(t, err)
	assert.Equal(t, "items.rank:DESC,items.id:DESC", order.Signature(), "the tiebreak is the copy")
}

func TestDefineSpecPanics(t *testing.T) {
	cases := map[string]struct {
		change func(*searchexec.SpecDef[item, itemSort, itemScope])
		want   string
	}{
		"no From":    {func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.From = nil }, "no From"},
		"no Columns": {func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Columns = nil }, "no Columns"},
		"no key":     {func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Keys = nil }, "no sort key"},
		"no tiebreak": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Tiebreak = nil },
			"no tiebreak",
		},
		"empty scope": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Scope, d.Context, d.Unscoped = nil, nil, nil },
			"no tenant scope",
		},
		"Unscoped entries only": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Scope, d.Context = nil, nil },
			"no tenant scope",
		},
		"zero ScopeCol": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Scope[1] = searchexec.ScopeCol[itemScope]{} },
			"Scope[1]",
		},
		"nil context scope": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Context = []searchexec.ContextScope{nil} },
			"Context[0] is nil",
		},
		"nil condition": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Conditions = []postgres.BoolExpression{nil} },
			"Conditions[0] is nil",
		},
		"default sort not a key": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Default = searchexec.Asc(itemSortName) },
			`default sort "name"`,
		},
		"limit 0": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Limit = 0 },
			"default limit 0",
		},
		"limit above the max": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) { d.Limit = search.MaxLimit + 1 },
			"default limit 1001",
		},
		"key name collides with the tiebreak": {
			func(d *searchexec.SpecDef[item, itemSort, itemScope]) {
				d.Keys[itemSortName] = keyset.IntExprKey("items.id", itemRank, func(r item) int64 { return r.Rank })
			},
			"items.id",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			def := itemDef()
			tc.change(&def)
			assert.Contains(t, panicMessage(t, func() { searchexec.DefineSpec(def) }), tc.want)
		})
	}

	t.Run("a context scope alone is a scope", func(t *testing.T) {
		def := itemDef()
		def.Scope = nil
		assert.NotPanics(t, func() { searchexec.DefineSpec(def) })
	})
	t.Run("a zero Default orders by the tiebreak", func(t *testing.T) {
		def := itemDef()
		def.Default = search.Sort[itemSort]{}
		spec := searchexec.DefineSpec(def)
		plan, err := searchexec.Resolve(t.Context(), spec, searchexec.New[itemSearch](spec, fullScope()).Query)
		require.NoError(t, err)
		assert.Empty(t, plan.Sorts())
		order, err := plan.Order(nil)
		require.NoError(t, err)
		assert.Equal(t, "items.id:ASC", order.Signature())
	})
}

func TestScopeConstructorsPanic(t *testing.T) {
	org := func(s itemScope) string { return s.Org }
	cases := map[string]struct {
		build func()
		want  string
	}{
		"ScopeEq with no column": {func() { searchexec.ScopeEq(org) }, "ScopeEq: no column"},
		"ScopeEq with a nil column": {
			func() { searchexec.ScopeEq(org, itemOrg, nil) },
			"ScopeEq: column 1 is nil",
		},
		"ScopeEq with no value": {
			func() { searchexec.ScopeEq[itemScope, string](nil, itemOrg) },
			"ScopeEq: value is nil",
		},
		"ScopeOrNull with a nil column": {func() { searchexec.ScopeOrNull(org, nil) }, "ScopeOrNull: column 0 is nil"},
		"ScopeOrNull with no value": {
			func() { searchexec.ScopeOrNull[itemScope, string](nil, itemOwner) },
			"ScopeOrNull: value is nil",
		},
		"ScopeNull with a nil column": {
			func() { searchexec.ScopeNull[itemScope](nil) },
			"ScopeNull: column 0 is nil",
		},
		"ScopeFunc with no function": {
			func() { searchexec.ScopeFunc[postgres.ColumnString](nil, itemWorkspace) },
			"ScopeFunc: fn is nil",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, panicMessage(t, tc.build), tc.want)
		})
	}
}

func TestScopeNullAndValue(t *testing.T) {
	type orgSearch struct {
		searchexec.Query[item, orgSearch, itemSort, string]
	}
	def := searchexec.SpecDef[item, itemSort, string]{
		From:    func(string) keyset.Source { return items },
		Columns: postgres.ColumnList{itemID, itemRank, itemOwner},
		Scope: []searchexec.ScopeCol[string]{
			searchexec.ScopeEq(searchexec.Value[string], itemOrg),
			searchexec.ScopeNull[string](itemOwner),
		},
		Keys:     itemDef().Keys,
		Tiebreak: itemDef().Tiebreak,
		Limit:    10,
	}
	spec := searchexec.DefineSpec(def)
	rec := &recorder{}
	q := searchexec.New[orgSearch](spec, "org-a")
	assert.Equal(t, "org-a", q.Scope())
	_, err := searchexec.Run(t.Context(), rec, spec, q.Query, identity)
	require.ErrorIs(t, err, errStop)
	assert.Contains(t, flat(rec.queries[0]), "WHERE ( (items.org = $1) AND (items.owner IS NULL) );")
	assert.Equal(t, []any{"org-a"}, rec.args[0])

	_, err = searchexec.Resolve(t.Context(), spec, searchexec.New[orgSearch](spec, "").Query)
	require.ErrorIs(t, err, search.ErrUnscoped)
}

// panicMessage returns the string that build panics with.
func panicMessage(t *testing.T, build func()) (message string) {
	t.Helper()
	defer func() {
		r := recover()
		require.NotNil(t, r, "no panic")
		s, ok := r.(string)
		require.True(t, ok, "panic value %v is not a string", r)
		message = s
	}()
	build()
	return ""
}

// fromScope is the scope of a search whose team value goes into an ON clause.
type fromScope struct {
	_    struct{}
	Org  string
	Team string
}

type fromSearch struct {
	searchexec.Query[item, fromSearch, itemSort, fromScope]
}

func fromDef() searchexec.SpecDef[item, itemSort, fromScope] {
	return searchexec.SpecDef[item, itemSort, fromScope]{
		From: func(s fromScope) keyset.Source {
			return items.LEFT_JOIN(teams, teamID.EQ(itemTeam).AND(teamOrg.EQ(postgres.String(s.Team))))
		},
		Columns: postgres.ProjectionList{itemID, itemRank, itemOwner},
		Scope: []searchexec.ScopeCol[fromScope]{
			searchexec.ScopeEq(func(s fromScope) string { return s.Org }, itemOrg),
		},
		Keys: map[itemSort]keyset.Key[item]{
			itemSortRank: keyset.IntKey(itemRank, func(r item) int64 { return r.Rank }),
		},
		Tiebreak: []keyset.Key[item]{keyset.StringKey(itemID, func(r item) string { return r.ID })},
		Limit:    20,
	}
}

func TestResolveRefusesAZeroFieldThatOnlyFromReads(t *testing.T) {
	spec := searchexec.DefineSpec(fromDef())
	rec := &recorder{}
	_, err := searchexec.Run(t.Context(), rec, spec,
		searchexec.New[fromSearch](spec, fromScope{Org: "secret-org"}).Query, identity)
	require.ErrorIs(t, err, search.ErrUnscoped)
	assert.Contains(t, err.Error(), "the scope field Team is zero")
	assert.NotContains(t, err.Error(), "secret-org")
	assert.Empty(t, rec.queries, "no statement runs")

	_, err = searchexec.Run(t.Context(), rec, spec,
		searchexec.New[fromSearch](spec, fromScope{Org: "org-a", Team: "team-1"}).Query, identity)
	require.ErrorIs(t, err, errStop)
	assert.Equal(t, []any{"team-1", "org-a"}, rec.args[0])
}

func TestScopeFrom(t *testing.T) {
	def := fromDef()
	def.Scope = []searchexec.ScopeCol[fromScope]{
		searchexec.ScopeFrom(func(s fromScope) string { return s.Org }, itemOrg),
		searchexec.ScopeFrom(func(s fromScope) string { return s.Team }, teamOrg),
	}
	def.From = func(s fromScope) keyset.Source {
		return items.INNER_JOIN(teams, teamID.EQ(itemTeam).
			AND(itemOrg.EQ(postgres.String(s.Org))).
			AND(teamOrg.EQ(postgres.String(s.Team))))
	}
	var spec *searchexec.Spec[item, itemSort, fromScope]
	require.NotPanics(t, func() { spec = searchexec.DefineSpec(def) }, "a ScopeFrom entry is a tenant scope")

	rec := &recorder{}
	_, err := searchexec.Run(t.Context(), rec, spec,
		searchexec.New[fromSearch](spec, fromScope{Team: "secret-team"}).Query, identity)
	require.ErrorIs(t, err, search.ErrUnscoped)
	assert.Contains(t, err.Error(), "the scope value of items.org is zero", "the error names the columns")
	assert.NotContains(t, err.Error(), "secret-team")
	assert.Empty(t, rec.queries, "no statement runs")

	_, err = searchexec.Run(t.Context(), rec, spec,
		searchexec.New[fromSearch](spec, fromScope{Org: "org-a", Team: "team-1"}).Query, identity)
	require.ErrorIs(t, err, errStop)
	assert.Contains(t, flat(rec.queries[0]), "WHERE $3;", "ScopeFrom adds no WHERE condition")
	assert.Equal(t, []any{"org-a", "team-1", true}, rec.args[0], "the plan runs with TRUE")

	assert.Contains(
		t,
		panicMessage(t, func() { searchexec.ScopeFrom[fromScope, string](nil, itemOrg) }),
		"value is nil",
	)
	assert.Contains(t,
		panicMessage(t, func() { searchexec.ScopeFrom(func(s fromScope) string { return s.Org }) }), "no column")
}
