package searchexec_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// The filter methods of itemSearch, as nano gen repo writes them. The fixture's table has no
// time column, so the time field reads a column of another table: the SQL test only reads
// the condition.

var (
	permItem = postgres.StringColumn("item_id")
	permOrg  = postgres.StringColumn("org")
	permName = postgres.StringColumn("name")
	perms    = postgres.NewTable("public", "perms", "", permItem, permOrg, permName)

	eventAt = postgres.TimestampzColumn("at")
	_       = postgres.NewTable("public", "events", "", eventAt)
)

func (q itemSearch) Name() searchexec.In[itemSearch, string] {
	return searchexec.NewIn(q.Query, itemName, searchexec.StringLit[string])
}

func (q itemSearch) NameFold() searchexec.InFold[itemSearch, string] {
	return searchexec.NewInFold[string](q.Query, itemName)
}

// Kind reads the name column as a native enum with the labels "a" and "b".
func (q itemSearch) Kind() searchexec.EqIn[itemSearch, string] {
	return searchexec.NewEqIn(q.Query, itemName, searchexec.EnumLit[string]("a", "b"))
}

func (q itemSearch) Rank() searchexec.EqIn[itemSearch, int64] {
	return searchexec.NewEqIn(q.Query, itemRank, searchexec.IntLit[int64])
}

func (q itemSearch) ID() searchexec.In[itemSearch, uuid.UUID] {
	return searchexec.NewIn(q.Query, itemID, searchexec.UUIDLit)
}

func (q itemSearch) Deleted() searchexec.Eq[itemSearch, bool] {
	return searchexec.NewEq(q.Query, itemDeleted, searchexec.BoolLit[bool])
}

func (q itemSearch) At() searchexec.TimeRange[itemSearch] {
	return searchexec.NewTimeRange(q.Query, eventAt)
}

func (q itemSearch) Owner() searchexec.Presence[itemSearch] {
	return searchexec.NewPresence(q.Query, itemOwner)
}

func (q itemSearch) Ranked() searchexec.Flag[itemSearch] {
	return searchexec.NewFlag(q.Query, itemRank.GT(postgres.Int(1)))
}

func (q itemSearch) Text() searchexec.Text[itemSearch] {
	return searchexec.NewText(q.Query, itemName, itemOwner)
}

// permCorrelation ties a permission to its item and to the item's organization.
var permCorrelation = permItem.EQ(itemID).AND(permOrg.EQ(itemOrg))

func (q itemSearch) Permission() searchexec.Exists[itemSearch, string] {
	return searchexec.NewExists(q.Query, perms, permCorrelation, permName, searchexec.StringLit[string])
}

func (q itemSearch) PermissionFold() searchexec.Exists[itemSearch, string] {
	return searchexec.NewExistsFold[string](q.Query, perms, permCorrelation, permName)
}

// filterSQL runs q and returns the part of the count statement after the scopes and the
// fixed condition, and the arguments of that part.
func filterSQL(t *testing.T, q itemSearch) (string, []any) {
	t.Helper()
	query, args := countSQL(t, t.Context(), q)
	const fixed = "(items.deleted IS FALSE) AND "
	flatQuery := flat(query)
	i := strings.Index(flatQuery, fixed)
	require.GreaterOrEqual(t, i, 0, flatQuery)
	return strings.TrimSuffix(flatQuery[i+len(fixed):], " );"), args[5:]
}

func TestFieldsSQL(t *testing.T) {
	q := newItemSearch(fullScope())
	id := uuid.MustParse("0b7e3f52-7a43-4e0e-9a39-6c1e2e1f2a10")
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		q    itemSearch
		sql  string
		args []any
	}{
		"In": {q.Name().In("a", "b"), "(items.name IN ($6, $7))", []any{"a", "b"}},
		"InFold": {
			q.NameFold().In("A", "b_%!"),
			"( (items.name ILIKE $6 ESCAPE '!') OR (items.name ILIKE $7 ESCAPE '!') )",
			[]any{"A", "b!_!%!!"},
		},
		"InFold one value": {q.NameFold().In(" A "), "(items.name ILIKE $6 ESCAPE '!')", []any{" A "}},
		"InFold, a value no column holds": {
			q.NameFold().In("a\x00", "b", "\xff"), "(items.name ILIKE $6 ESCAPE '!')", []any{"b"},
		},
		"InFold, only values no column holds": {q.NameFold().In("a\x00", "\xc3"), "FALSE", []any{}},
		"In, a value no column holds": {
			q.Name().In("a", "b\x00", "\xff"), "(items.name IN ($6, NULL, NULL))", []any{"a"},
		},
		"Enum In": {
			q.Kind().In("a", "b"), "(items.name IN ($6, $7))", []any{"a", "b"},
		},
		"Enum In, a value that is not a label": {
			q.Kind().In("a", "bogus", "A"), "(items.name IN ($6, NULL, NULL))", []any{"a"},
		},
		"Enum Eq, a value that is not a label": {
			q.Kind().Eq("bogus"), "(items.name = NULL)", []any{},
		},
		"EqIn Eq": {q.Rank().Eq(3), "(items.rank = $6)", []any{int64(3)}},
		"EqIn In": {q.Rank().In(1, 2), "(items.rank IN ($6, $7))", []any{int64(1), int64(2)}},
		"UUID":    {q.ID().In(id), "(items.id IN ($6::uuid))", []any{id.String()}},
		"Eq":      {q.Deleted().Eq(true), "(items.deleted = $6)", []any{true}},
		"Since":   {q.At().Since(at), "(events.at >= $6)", []any{at}},
		"Until":   {q.At().Until(at), "(events.at <= $6)", []any{at}},
		"After":   {q.At().After(at), "(events.at > $6)", []any{at}},
		"Before":  {q.At().Before(at), "(events.at < $6)", []any{at}},
		// A bound with nanoseconds moves to the microsecond that keeps its meaning.
		"Since +1ns":  {q.At().Since(at.Add(time.Nanosecond)), "(events.at >= $6)", []any{at.Add(time.Microsecond)}},
		"Until +1ns":  {q.At().Until(at.Add(time.Nanosecond)), "(events.at <= $6)", []any{at}},
		"After +1ns":  {q.At().After(at.Add(time.Nanosecond)), "(events.at > $6)", []any{at}},
		"Before +1ns": {q.At().Before(at.Add(time.Nanosecond)), "(events.at < $6)", []any{at.Add(time.Microsecond)}},
		"Since -1ns":  {q.At().Since(at.Add(-time.Nanosecond)), "(events.at >= $6)", []any{at}},
		"Until -1ns": {
			q.At().Until(at.Add(-time.Nanosecond)), "(events.at <= $6)", []any{at.Add(-time.Microsecond)},
		},
		"Present true":  {q.Owner().Present(true), "(items.owner IS NOT NULL)", []any{}},
		"Present false": {q.Owner().Present(false), "(items.owner IS NULL)", []any{}},
		"Is true":       {q.Ranked().Is(true), "(items.rank > $6)", []any{int64(1)}},
		"Is false":      {q.Ranked().Is(false), "((items.rank > $6) IS NOT TRUE)", []any{int64(1)}},
		"Contains": {
			q.Text().Contains(" a_ "),
			"( (items.name ILIKE $6 ESCAPE '!') OR (items.owner ILIKE $7 ESCAPE '!') )",
			[]any{"%a!_%", "%a!_%"},
		},
		"ContainsAny": {
			q.Text().ContainsAny("x", "y"),
			"( ( (items.name ILIKE $6 ESCAPE '!') OR (items.owner ILIKE $7 ESCAPE '!') ) OR " +
				"( (items.name ILIKE $8 ESCAPE '!') OR (items.owner ILIKE $9 ESCAPE '!') ) )",
			[]any{"%x%", "%x%", "%y%", "%y%"},
		},
		"Exists": {
			q.Permission().In("read"),
			"(EXISTS ( SELECT 1 FROM public.perms WHERE ( ((perms.item_id = items.id) AND (perms.org = items.org)) " +
				"AND (perms.name IN ($6)) ) ))",
			[]any{"read"},
		},
		"Exists, a value no column holds": {
			q.Permission().In("r\x00"),
			"(EXISTS ( SELECT 1 FROM public.perms WHERE ( ((perms.item_id = items.id) AND (perms.org = items.org)) " +
				"AND (perms.name IN (NULL)) ) ))",
			[]any{},
		},
		"Exists fold": {
			q.PermissionFold().In("Read"),
			"(EXISTS ( SELECT 1 FROM public.perms WHERE ( ((perms.item_id = items.id) AND (perms.org = items.org)) " +
				"AND (perms.name ILIKE $6 ESCAPE '!') ) ))",
			[]any{"Read"},
		},
		"two fields": {
			q.Name().In("a").Owner().Present(true),
			"(items.name IN ($6)) AND (items.owner IS NOT NULL)",
			[]any{"a"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sql, args := filterSQL(t, tc.q)
			assert.Equal(t, tc.sql, sql)
			assert.Equal(t, tc.args, args)
		})
	}
}

// From Go, an empty list matches no row: a caller that builds an empty list gets nothing,
// not everything.
func TestEmptyListsMatchNoRow(t *testing.T) {
	q := newItemSearch(fullScope())
	for name, empty := range map[string]itemSearch{
		"In":          q.Name().In(),
		"InFold":      q.NameFold().In(),
		"EqIn":        q.Rank().In(),
		"ContainsAny": q.Text().ContainsAny(),
		"Exists":      q.Permission().In(),
		"Exists fold": q.PermissionFold().In(),
	} {
		t.Run(name, func(t *testing.T) {
			sql, args := filterSQL(t, empty)
			assert.Equal(t, "FALSE", sql)
			assert.Empty(t, args)
		})
	}
}

func values(n int) []string {
	vs := make([]string, n)
	for i := range vs {
		vs[i] = "v"
	}
	return vs
}

func TestLimitsAreRecordedAndResolveReturnsThem(t *testing.T) {
	q := newItemSearch(fullScope())
	atLimit, overLimit := values(search.MaxInValues), values(search.MaxInValues+1)
	ranks := make([]int64, search.MaxInValues+1)
	longTerm := strings.Repeat("é", search.MaxTermLength) // 1000 characters, 2000 bytes

	accepted := map[string]itemSearch{
		"In at the limit":              q.Name().In(atLimit...),
		"InFold at the limit":          q.NameFold().In(atLimit...),
		"EqIn at the limit":            q.Rank().In(ranks[1:]...),
		"Exists at the limit":          q.Permission().In(atLimit...),
		"ContainsAny at the limit":     q.Text().ContainsAny(atLimit...),
		"a term of 1000 characters":    q.Text().Contains(longTerm),
		"padding is not counted":       q.Text().Contains("  " + longTerm + "\t\n"),
		"ContainsAny, 1000 characters": q.Text().ContainsAny("a", longTerm),
	}
	for name, ok := range accepted {
		t.Run(name, func(t *testing.T) {
			_, err := searchexec.Resolve(t.Context(), itemSpec, ok.Query)
			require.NoError(t, err)
		})
	}

	refused := map[string]struct {
		q    itemSearch
		want error
	}{
		"In":                   {q.Name().In(overLimit...), search.ErrTooManyValues},
		"InFold":               {q.NameFold().In(overLimit...), search.ErrTooManyValues},
		"EqIn":                 {q.Rank().In(ranks...), search.ErrTooManyValues},
		"Exists":               {q.Permission().In(overLimit...), search.ErrTooManyValues},
		"Exists fold":          {q.PermissionFold().In(overLimit...), search.ErrTooManyValues},
		"ContainsAny terms":    {q.Text().ContainsAny(overLimit...), search.ErrTooManyValues},
		"Contains":             {q.Text().Contains(longTerm + "x"), search.ErrTermTooLong},
		"ContainsAny one term": {q.Text().ContainsAny("a", longTerm+"x"), search.ErrTermTooLong},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{}
			_, err := searchexec.Run(t.Context(), rec, itemSpec, tc.q.Query, identity)
			require.ErrorIs(t, err, tc.want)
			assert.True(t, search.IsClientError(err))
			assert.Empty(t, rec.queries, "no statement runs")
		})
	}
}

func TestLimitErrorsNameNoValue(t *testing.T) {
	q := newItemSearch(fullScope())
	_, err := searchexec.Resolve(
		t.Context(),
		itemSpec,
		q.Name().In(append(values(search.MaxInValues), "secret")...).Query,
	)
	require.ErrorIs(t, err, search.ErrTooManyValues)
	assert.Equal(t, "search: too many values: items.name has 1001 values, the limit is 1000", err.Error())

	_, err = searchexec.Resolve(t.Context(), itemSpec,
		q.Text().Contains("secret"+strings.Repeat("x", search.MaxTermLength)).Query)
	require.ErrorIs(t, err, search.ErrTermTooLong)
	assert.Equal(t, "search: term too long: a term of 1006 characters, the limit is 1000", err.Error())
}

func TestRecordedErrorOrder(t *testing.T) {
	over := values(search.MaxInValues + 1)
	long := strings.Repeat("x", search.MaxTermLength+1)

	// A missing tenant scope is a programming error, and it wins over a client error.
	unscoped := newItemSearch(itemScope{Org: "org-a", Team: "team-1"}).Name().In(over...)
	_, err := searchexec.Resolve(t.Context(), itemSpec, unscoped.Query)
	require.ErrorIs(t, err, search.ErrUnscoped)
	assert.NotErrorIs(t, err, search.ErrTooManyValues)

	// The first recorded error wins; a later filter neither replaces it nor runs.
	q := newItemSearch(fullScope()).Name().In(over...).Text().Contains(long).Owner().Present(true)
	_, err = searchexec.Resolve(t.Context(), itemSpec, q.Query)
	require.ErrorIs(t, err, search.ErrTooManyValues)
	assert.NotErrorIs(t, err, search.ErrTermTooLong)

	// A recorded error comes before the page check.
	_, err = searchexec.Resolve(t.Context(), itemSpec, q.Limit(0).Query)
	require.ErrorIs(t, err, search.ErrTooManyValues)

	// The search the error was recorded on stays valid.
	base := newItemSearch(fullScope()).Name().In("a")
	_ = base.Name().In(over...)
	sql, _ := filterSQL(t, base)
	assert.Equal(t, "(items.name IN ($6))", sql)
}

// A field of a struct literal of the builder returns the zero builder, which Resolve
// refuses. No field method panics on it.
func TestFieldsOfAStructLiteral(t *testing.T) {
	var q itemSearch
	at := time.Now()
	for name, got := range map[string]itemSearch{
		"In":          q.Name().In("a"),
		"In over":     q.Name().In(values(search.MaxInValues + 1)...),
		"InFold":      q.NameFold().In("a"),
		"EqIn":        q.Rank().Eq(1),
		"Eq":          q.Deleted().Eq(true),
		"Since":       q.At().Since(at),
		"Until":       q.At().Until(at),
		"After":       q.At().After(at),
		"Before":      q.At().Before(at),
		"Present":     q.Owner().Present(true),
		"Absent":      q.Owner().Present(false),
		"Is":          q.Ranked().Is(true),
		"Is not":      q.Ranked().Is(false),
		"Contains":    q.Text().Contains("a"),
		"ContainsAny": q.Text().ContainsAny("a"),
		"Exists":      q.Permission().In("a"),
		"zero field":  searchexec.In[itemSearch, string]{}.In("a"),
		"zero text":   searchexec.Text[itemSearch]{}.ContainsAny(values(search.MaxInValues + 1)...),
		"zero flag":   searchexec.Flag[itemSearch]{}.Is(false),
		"zero exists": searchexec.Exists[itemSearch, string]{}.In("a"),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, itemSearch{}, got)
			_, err := searchexec.Resolve(t.Context(), itemSpec, got.Query)
			require.ErrorIs(t, err, search.ErrUnscoped)
		})
	}
}

// Two searches built from one search do not share the backing array of its filters.
func TestFieldsKeepTheQueryImmutable(t *testing.T) {
	base := newItemSearch(fullScope()).Name().In("a").Rank().Eq(1).Owner().Present(true)
	left := base.Deleted().Eq(true)
	right := base.Deleted().Eq(false)

	sql, args := filterSQL(t, base)
	assert.Equal(t, "(items.name IN ($6)) AND (items.rank = $7) AND (items.owner IS NOT NULL)", sql)
	assert.Equal(t, []any{"a", int64(1)}, args)
	_, args = filterSQL(t, left)
	assert.Equal(t, []any{"a", int64(1), true}, args)
	_, args = filterSQL(t, right)
	assert.Equal(t, []any{"a", int64(1), false}, args)
}

func TestExistsRefusesAChildOfAnotherDialect(t *testing.T) {
	child := mysql.NewTable("db", "perms", "", mysql.StringColumn("name"))
	q := newItemSearch(fullScope())
	f := searchexec.NewExists(
		q.Query,
		keyset.Source(child),
		postgres.Bool(true),
		permName,
		searchexec.StringLit[string],
	)
	_, err := searchexec.Resolve(t.Context(), itemSpec, f.In("read").Query)
	require.ErrorIs(t, err, keyset.ErrDialect)
}

func TestUUIDLitPerDialect(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("0B7E3F52-7A43-4E0E-9A39-6C1E2E1F2A10")
	got := where(name.EQ(searchexec.UUIDLit(id)))
	assert.Equal(t, statementSQL{"\nSELECT $1\nWHERE name = $2::uuid;\n", []any{id.String()}}, got["postgres"])
	text := statementSQL{"\nSELECT ?\nWHERE name = ?;\n", []any{"0b7e3f52-7a43-4e0e-9a39-6c1e2e1f2a10"}}
	assert.Equal(t, text, got["mysql"])
	assert.Equal(t, text, got["sqlite"])
}

func TestContainsAny(t *testing.T) {
	t.Parallel()

	for dialect, got := range where(searchexec.ContainsAny(nil, name)) {
		assert.True(t, strings.HasSuffix(got.query, "\nWHERE FALSE;\n"), dialect)
		assert.Empty(t, got.args, dialect)
	}
	assert.Equal(
		t,
		where(searchexec.Contains("ab", name, note)),
		where(searchexec.ContainsAny([]string{"ab"}, name, note)),
		"one term is Contains",
	)
	pg := where(searchexec.ContainsAny([]string{"a", " "}, name))["postgres"]
	assert.Equal(t, "SELECT $1 WHERE ( (name ILIKE $2 ESCAPE '!') OR TRUE );", flat(pg.query),
		"a blank term matches every row")

	assert.Equal(t, "searchexec: Contains needs at least one expression",
		panicMessage(t, func() { searchexec.ContainsAny(nil) }), "no term does not skip the check of exprs")
	assert.Equal(t, "searchexec: Contains expression 1 is nil",
		panicMessage(t, func() { searchexec.ContainsAny([]string{"a", "b"}, name, nil) }))
}

func TestFieldConstructorsPanic(t *testing.T) {
	q := newItemSearch(fullScope()).Query
	lit := searchexec.StringLit[string]
	var nilLit func(string) postgres.StringExpression
	cases := map[string]struct {
		build func()
		want  string
	}{
		"In nil expression": {func() { searchexec.NewIn(q, nil, lit) }, "searchexec: NewIn: the expression is nil"},
		"In nil literal": {
			func() { searchexec.NewIn(q, itemName, nilLit) },
			"searchexec: NewIn: the literal function is nil",
		},
		"InFold nil expression": {
			func() { searchexec.NewInFold[string](q, nil) },
			"searchexec: NewInFold: the expression is nil",
		},
		"Eq nil expression":   {func() { searchexec.NewEq(q, nil, lit) }, "searchexec: NewEq: the expression is nil"},
		"EqIn nil expression": {func() { searchexec.NewEqIn(q, nil, lit) }, "searchexec: NewEq: the expression is nil"},
		"TimeRange nil": {
			func() { searchexec.NewTimeRange(q, nil) },
			"searchexec: NewTimeRange: the expression is nil",
		},
		"Presence nil": {
			func() { searchexec.NewPresence(q, nil) },
			"searchexec: NewPresence: the expression is nil",
		},
		"Flag nil":           {func() { searchexec.NewFlag(q, nil) }, "searchexec: NewFlag: the expression is nil"},
		"Text no expression": {func() { searchexec.NewText(q) }, "searchexec: NewText: no expression"},
		"Text nil expression": {
			func() { searchexec.NewText(q, itemName, nil) }, "searchexec: NewText: expression 1 is nil",
		},
		"Exists nil child": {
			func() { searchexec.NewExists(q, nil, permCorrelation, permName, lit) },
			"searchexec: NewExists: the child is nil",
		},
		"Exists nil correlation": {
			func() { searchexec.NewExists(q, perms, nil, permName, lit) },
			"searchexec: NewExists: the correlation is nil",
		},
		"Exists nil column": {
			func() { searchexec.NewExists(q, perms, permCorrelation, nil, lit) },
			"searchexec: NewExists: the expression is nil",
		},
		"ExistsFold nil column": {
			func() { searchexec.NewExistsFold[string](q, perms, permCorrelation, nil) },
			"searchexec: NewExistsFold: the expression is nil",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, panicMessage(t, tc.build))
		})
	}
}

// NewText keeps its own copy of the expressions, so a caller that changes its slice later
// does not change the field.
func TestNewTextCopiesTheExpressions(t *testing.T) {
	exprs := []postgres.StringExpression{itemName}
	f := searchexec.NewText(newItemSearch(fullScope()).Query, exprs...)
	exprs[0] = itemOwner
	sql, _ := filterSQL(t, f.Contains("a"))
	assert.Equal(t, "(items.name ILIKE $6 ESCAPE '!')", sql)
	assert.True(t, slices.Equal([]postgres.StringExpression{itemOwner}, exprs))
}
