// This file is Nanostack-only (not in go-jet): the db/searchexec field types against
// PostgreSQL, on a builder written by hand as nano gen repo writes it. See db/UPSTREAM.md.

package postgres

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

type sfRow struct {
	ID int64 `alias:"sf_row.id"`
}

type sfSort string

const sfID sfSort = "id"

// sfSearch is a search of items scoped to one organization.
type sfSearch struct {
	searchexec.Query[sfRow, sfSearch, sfSort, string]
}

// sfT0 is the bound of the time tests, in a zone that is not UTC.
var sfT0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.FixedZone("UTC-5", -5*3600))

// sfItem is one row of the items table.
type sfItem struct {
	id     int64
	org    string
	name   string
	kind   string
	n      int64
	active bool
	at     time.Time
	note   *string
	score  *int64
}

func sfText(s string) *string { return &s }
func sfInt(n int64) *int64    { return &n }

// The rows of org-a, and rows of org-b that match the same filters, so only the scope drops
// them. Rows 2 and 4 are exactly on sfT0.
var sfItems = []sfItem{
	{1, "org-a", "İSTANBUL", "x", 1, true, sfT0.Add(-time.Second), sfText("hello"), sfInt(1)},
	{2, "org-a", "istanbul", "y", 2, false, sfT0, nil, nil},
	{3, "org-a", "ISTANBUL", "z", 3, true, sfT0.Add(time.Second), sfText("Wörld_%"), sfInt(2)},
	{4, "org-a", "ıstanbul", "x", 4, false, sfT0, sfText(""), sfInt(3)},
	{5, "org-a", "Paris", "y", 5, true, sfT0.Add(2 * time.Second), sfText("x!y"), nil},
	{101, "org-b", "istanbul", "x", 1, true, sfT0, sfText("hello"), sfInt(2)},
	{102, "org-b", "Paris", "y", 5, false, sfT0.Add(time.Second), nil, nil},
}

// sfUID is the uuid of row id.
func sfUID(id int64) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(id)})
}

// sfPerms are the child rows: (org, item, name). The org-b permission on item 2 is a row of
// another tenant on an org-a item: only the tenant part of the correlation drops it. The
// permission "İ" tests the fold of a child column in "C".
var sfPerms = []struct {
	org  string
	item int64
	name string
}{
	{"org-a", 1, "read"},
	{"org-a", 3, "Write"},
	{"org-b", 2, "write"},
	{"org-b", 101, "read"},
	{"org-a", 4, "İ"},
}

type sfFixture struct {
	itemsName string

	items, perms                     Table
	id, n, score, permItem           ColumnInteger
	org, name, nameC, kind, note     ColumnString
	uid                              ColumnString
	permOrg, permName, permNameC     ColumnString
	active                           ColumnBool
	at                               ColumnTimestampz
	spec                             *searchexec.Spec[sfRow, sfSort, string]
	correlation, correlationNoTenant BoolExpression
	cCollation                       bool
}

func newSfFixture(t *testing.T) *sfFixture {
	t.Helper()

	suffix := strings.ToLower(rand.Text())
	f := &sfFixture{itemsName: "nanostack_sf_items_" + suffix}
	permsName := "nanostack_sf_perms_" + suffix
	// CockroachDB has no "C" collation, so there name_c is plain TEXT.
	f.cCollation = !sourceIsCockroachDB()
	cCollate := ""
	if f.cCollation {
		cCollate = ` COLLATE "C"`
	}
	for _, stmt := range []string{
		"CREATE TABLE test_sample." + f.itemsName + ` (
			id BIGINT PRIMARY KEY,
			org TEXT NOT NULL,
			name TEXT NOT NULL,
			name_c TEXT` + cCollate + ` NOT NULL,
			kind TEXT NOT NULL,
			uid UUID NOT NULL,
			n BIGINT NOT NULL,
			active BOOLEAN NOT NULL,
			at TIMESTAMPTZ NOT NULL,
			note TEXT,
			score BIGINT
		)`,
		"CREATE TABLE test_sample." + permsName + ` (
			org TEXT NOT NULL,
			item_id BIGINT NOT NULL REFERENCES test_sample.` + f.itemsName + ` (id),
			name TEXT NOT NULL,
			name_c TEXT` + cCollate + ` NOT NULL
		)`,
	} {
		_, err := db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		for _, name := range []string{permsName, f.itemsName} {
			_, err := db.ExecContext(context.Background(), "DROP TABLE test_sample."+name)
			require.NoError(t, err)
		}
	})
	for _, r := range sfItems {
		_, err := db.ExecContext(ctx, "INSERT INTO test_sample."+f.itemsName+
			" (id, org, name, name_c, kind, uid, n, active, at, note, score)"+
			" VALUES ($1, $2, $3, $3, $4, $5, $6, $7, $8, $9, $10)",
			r.id, r.org, r.name, r.kind, sfUID(r.id).String(), r.n, r.active, r.at, r.note, r.score)
		require.NoError(t, err)
	}
	for _, p := range sfPerms {
		_, err := db.ExecContext(ctx, "INSERT INTO test_sample."+permsName+
			" (org, item_id, name, name_c) VALUES ($1, $2, $3, $3)", p.org, p.item, p.name)
		require.NoError(t, err)
	}

	f.id, f.n, f.score = IntegerColumn("id"), IntegerColumn("n"), IntegerColumn("score")
	f.org, f.name, f.nameC = StringColumn("org"), StringColumn("name"), StringColumn("name_c")
	f.kind, f.note, f.uid = StringColumn("kind"), StringColumn("note"), StringColumn("uid")
	f.active, f.at = BoolColumn("active"), TimestampzColumn("at")
	f.items = NewTable("test_sample", f.itemsName, "",
		f.id, f.org, f.name, f.nameC, f.kind, f.uid, f.n, f.active, f.at, f.note, f.score)
	f.permOrg, f.permItem = StringColumn("org"), IntegerColumn("item_id")
	f.permName, f.permNameC = StringColumn("name"), StringColumn("name_c")
	f.perms = NewTable("test_sample", permsName, "", f.permOrg, f.permItem, f.permName, f.permNameC)
	f.correlationNoTenant = f.permItem.EQ(f.id)
	f.correlation = f.correlationNoTenant.AND(f.permOrg.EQ(f.org))

	key := keyset.IntKey(f.id, func(r sfRow) int64 { return r.ID })
	f.spec = searchexec.DefineSpec(searchexec.SpecDef[sfRow, sfSort, string]{
		From:     func(string) keyset.Source { return f.items },
		Columns:  ProjectionList{f.id.AS("sf_row.id")},
		Scope:    []searchexec.ScopeCol[string]{searchexec.ScopeEq(searchexec.Value[string], f.org)},
		Keys:     map[sfSort]keyset.Key[sfRow]{sfID: key},
		Tiebreak: []keyset.Key[sfRow]{key},
		Default:  searchexec.Asc(sfID),
		Limit:    search.MaxLimit,
	})
	return f
}

func (f *sfFixture) search(org string) sfSearch { return searchexec.New[sfSearch](f.spec, org) }

// ids runs q and returns the ids of every match, in id order.
func (f *sfFixture) ids(t *testing.T, q sfSearch) []int64 {
	t.Helper()
	result, err := searchexec.Run(t.Context(), db, f.spec, q.Query, func(r sfRow) int64 { return r.ID })
	require.NoError(t, err)
	require.Empty(t, result.NextCursor, "one page holds every match")
	if result.Items == nil {
		return []int64{}
	}
	return result.Items
}

// The org-a rows, for the tests where a filter keeps every row.
var sfAllA = []int64{1, 2, 3, 4, 5}

func TestSearchexecFieldsBothWays(t *testing.T) {
	f := newSfFixture(t)
	q := f.search("org-a")
	kind := searchexec.NewEqIn(q.Query, f.kind, searchexec.StringLit[string])
	n := searchexec.NewIn(q.Query, f.n, searchexec.IntLit[int64])
	uid := searchexec.NewIn(q.Query, f.uid, searchexec.UUIDLit)
	active := searchexec.NewEq(q.Query, f.active, searchexec.BoolLit[bool])
	name := searchexec.NewIn(q.Query, f.name, searchexec.StringLit[string])
	nameFold := searchexec.NewInFold[string](q.Query, f.name)
	perm := searchexec.NewExists(q.Query, f.perms, f.correlation, f.permName, searchexec.StringLit[string])
	at := searchexec.NewTimeRange(q.Query, f.at)
	note := searchexec.NewPresence(q.Query, f.note)
	ranked := searchexec.NewFlag(q.Query, f.score.GT(Int(1)))
	text := searchexec.NewText(q.Query, f.name, f.note)

	cases := map[string]struct {
		q    sfSearch
		want []int64
	}{
		"Eq keeps":                 {kind.Eq("x"), []int64{1, 4}},
		"Eq drops":                 {kind.Eq("none"), []int64{}},
		"EqIn In":                  {kind.In("x", "z", "none"), []int64{1, 3, 4}},
		"In int":                   {n.In(2, 5), []int64{2, 5}},
		"In int drops":             {n.In(9), []int64{}},
		"In uuid":                  {uid.In(sfUID(3), sfUID(101)), []int64{3}},
		"In uuid drops":            {uid.In(uuid.New()), []int64{}},
		"Eq bool true":             {active.Eq(true), []int64{1, 3, 5}},
		"Eq bool false":            {active.Eq(false), []int64{2, 4}},
		"In is exact":              {name.In("istanbul"), []int64{2}},
		"In of no value":           {name.In(), []int64{}},
		"Since: on the bound":      {at.Since(sfT0), []int64{2, 3, 4, 5}},
		"After: not on the bound":  {at.After(sfT0), []int64{3, 5}},
		"Until: on the bound":      {at.Until(sfT0), []int64{1, 2, 4}},
		"Before: not on the bound": {at.Before(sfT0), []int64{1}},
		"Since and Before": {
			searchexec.NewTimeRange(at.Since(sfT0).Query, f.at).Before(sfT0.Add(2 * time.Second)), []int64{2, 3, 4},
		},
		// A bound with nanoseconds: the same answer with lib/pq and pgx.
		"Since t0+600ns":  {at.Since(sfT0.Add(600 * time.Nanosecond)), []int64{3, 5}},
		"Since t0+400ns":  {at.Since(sfT0.Add(400 * time.Nanosecond)), []int64{3, 5}},
		"Before t0+400ns": {at.Before(sfT0.Add(400 * time.Nanosecond)), []int64{1, 2, 4}},
		"Until t0-400ns":  {at.Until(sfT0.Add(-400 * time.Nanosecond)), []int64{1}},
		"After t0-600ns":  {at.After(sfT0.Add(-600 * time.Nanosecond)), []int64{2, 3, 4, 5}},
		// A value that no text column holds matches no row, and gives no error.
		"In, NUL":                        {name.In("a\x00b"), []int64{}},
		"In, invalid UTF-8 and a value":  {name.In("Paris", "\xff"), []int64{5}},
		"Eq, NUL":                        {kind.Eq("x\x00"), []int64{}},
		"InFold, NUL":                    {nameFold.In("a\x00"), []int64{}},
		"InFold, invalid UTF-8, a value": {nameFold.In("paris", "\xc3"), []int64{5}},
		"Exists, NUL":                    {perm.In("read\x00"), []int64{}},
		"Present true":                   {note.Present(true), []int64{1, 3, 4, 5}},
		"Present false":                  {note.Present(false), []int64{2}},
		"Is true":                        {ranked.Is(true), []int64{3, 4}},
		"Is false, NULL included":        {ranked.Is(false), []int64{1, 2, 5}},
		"Contains":                       {text.Contains("WÖRLD_"), []int64{3}},
		"Contains wildcards":             {text.Contains("_%"), []int64{3}},
		"Contains the escape":            {text.Contains("!"), []int64{5}},
		"Contains drops":                 {text.Contains("zzz"), []int64{}},
		"Contains blank":                 {text.Contains("  "), sfAllA},
		"ContainsAny":                    {text.ContainsAny("paris", "HELLO"), []int64{1, 5}},
		"ContainsAny drops":              {text.ContainsAny("zzz", "yyy"), []int64{}},
		"ContainsAny of no term":         {text.ContainsAny(), []int64{}},
		"ContainsAny, a blank term":      {text.ContainsAny("zzz", " "), sfAllA},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, f.ids(t, tc.q))
		})
	}
}

// InFold folds both sides in the database, by the rule of the column's collation. Go's
// strings.ToLower and the database's LOWER differ on the Turkish dotted I under COLLATE
// "C", so a fold in Go would give another answer there. LOWER($1) alone would fold the value
// by the default collation, not the column's, so the C column would not find its own rows.
func TestSearchexecInFoldAgreesWithTheColumn(t *testing.T) {
	f := newSfFixture(t)
	columns := map[string]ColumnString{"name": f.name, "name_c": f.nameC}
	inFold := func(expr ColumnString, term string) []int64 {
		return f.ids(t, searchexec.NewInFold[string](f.search("org-a").Query, expr).In(term))
	}

	t.Run("each row finds itself", func(t *testing.T) {
		for _, it := range sfItems[:5] {
			for column, expr := range columns {
				assert.Contains(t, inFold(expr, it.name), it.id, "%s %s", column, it.name)
			}
		}
	})

	terms := []string{"İSTANBUL", "istanbul", "ISTANBUL", "ıstanbul", "i̇stanbul", "PARIS", "İ%", "_stanbul"}
	for column, expr := range columns {
		for _, term := range terms {
			t.Run(column+" "+term, func(t *testing.T) {
				assert.Equal(t, f.lowerEquals(t, column, term), inFold(expr, term))
			})
		}
	}

	t.Run(`COLLATE "C" folds ASCII letters only`, func(t *testing.T) {
		if !f.cCollation {
			t.Skip("CockroachDB has no C collation")
		}
		require.Equal(t, "istanbul", strings.ToLower("İSTANBUL"), "Go folds the dotted I")
		assert.Equal(t, []int64{2, 3}, inFold(f.nameC, "istanbul"), "the database does not, under C")
		assert.Equal(t, []int64{1}, inFold(f.nameC, "İSTANBUL"), "the value folds by C too")
	})

	t.Run("Exists fold", func(t *testing.T) {
		got := f.ids(t, searchexec.NewExistsFold[string](f.search("org-a").Query, f.perms, f.correlation, f.permName).
			In("WRITE", "READ"))
		assert.Equal(t, []int64{1, 3}, got)
	})

	t.Run(`Exists fold, a child column in "C"`, func(t *testing.T) {
		exists := func(term string) []int64 {
			return f.ids(t, searchexec.NewExistsFold[string](f.search("org-a").Query, f.perms, f.correlation,
				f.permNameC).In(term))
		}
		assert.Equal(t, []int64{4}, exists("İ"))
		if f.cCollation {
			assert.Equal(t, []int64{}, exists("i"), "C does not fold the dotted I")
		}
	})
}

// lowerEquals returns the org-a ids where LOWER(column) = LOWER(term), from a statement
// written by hand. The term takes the collation of the column, so both sides fold by the
// same rule.
func (f *sfFixture) lowerEquals(t *testing.T, column, term string) []int64 {
	t.Helper()
	value := "$1::text"
	if column == "name_c" && f.cCollation {
		value = `$1::text COLLATE "C"`
	}
	rows, err := db.QueryContext(ctx, "SELECT id FROM test_sample."+f.itemsName+
		" WHERE org = 'org-a' AND LOWER("+column+") = LOWER("+value+") ORDER BY id", term)
	require.NoError(t, err)
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

// Exists on a child table of two tenants: a child row of another tenant never makes a row
// match, and a row of another tenant never shows.
func TestSearchexecExistsTwoTenants(t *testing.T) {
	f := newSfFixture(t)
	exists := func(org string, correlation BoolExpression) searchexec.Exists[sfSearch, string] {
		return searchexec.NewExists(f.search(org).Query, f.perms, correlation, f.permName, searchexec.StringLit[string])
	}
	cases := map[string]struct {
		q    sfSearch
		want []int64
	}{
		"org-a read":                           {exists("org-a", f.correlation).In("read"), []int64{1}},
		"org-a Write":                          {exists("org-a", f.correlation).In("Write"), []int64{3}},
		"In is exact":                          {exists("org-a", f.correlation).In("WRITE"), []int64{}},
		"a child row of org-b on an org-a row": {exists("org-a", f.correlation).In("write"), []int64{}},
		"org-b read":                           {exists("org-b", f.correlation).In("read"), []int64{101}},
		"org-b write: the row is org-a's":      {exists("org-b", f.correlation).In("write"), []int64{}},
		"no value":                             {exists("org-a", f.correlation).In(), []int64{}},
		// The fixture discriminates: without its tenant part, the correlation lets the
		// org-b child row through.
		"no tenant in the correlation": {exists("org-a", f.correlationNoTenant).In("write"), []int64{2}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, f.ids(t, tc.q))
		})
	}
}

func TestSearchexecFieldLimits(t *testing.T) {
	f := newSfFixture(t)
	q := f.search("org-a")
	names := make([]string, search.MaxInValues+1)
	for i := range names {
		names[i] = "v" + strings.Repeat("x", i%7)
	}
	names[0] = "Paris"
	term := strings.Repeat("é", search.MaxTermLength)
	in := searchexec.NewIn(q.Query, f.name, searchexec.StringLit[string])
	inFold := searchexec.NewInFold[string](q.Query, f.name)
	text := searchexec.NewText(q.Query, f.name)

	t.Run("at the limit, the statement runs", func(t *testing.T) {
		assert.Equal(t, []int64{5}, f.ids(t, in.In(names[:search.MaxInValues]...)))
		assert.Equal(t, []int64{5}, f.ids(t, inFold.In(names[:search.MaxInValues]...)))
		assert.Equal(t, []int64{}, f.ids(t, text.Contains(term)))
	})

	for name, tc := range map[string]struct {
		q    sfSearch
		want error
	}{
		"1001 values":           {in.In(names...), search.ErrTooManyValues},
		"1001 folded values":    {inFold.In(names...), search.ErrTooManyValues},
		"1001 terms":            {text.ContainsAny(names...), search.ErrTooManyValues},
		"a 1001-character term": {text.Contains(term + "e"), search.ErrTermTooLong},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := searchexec.Run(t.Context(), db, f.spec, tc.q.Query, func(r sfRow) int64 { return r.ID })
			require.ErrorIs(t, err, tc.want)
			assert.True(t, search.IsClientError(err))
		})
	}
}
