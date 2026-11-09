// This file is Nanostack-only (not in go-jet): the checks of the db/searchexec field types
// (R7) on every dialect. See db/UPSTREAM.md.

package searchexeccontract

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

func (s suite) fieldsBothWays(t *testing.T, f *sfFixture) {
	q := f.search("org-a")
	kind := searchexec.NewEqIn(q.Query, f.kind, searchexec.StringLit[string])
	n := searchexec.NewIn(q.Query, f.n, searchexec.IntLit[int64])
	uid := searchexec.NewIn(q.Query, f.uid, searchexec.UUIDLit)
	active := searchexec.NewEq(q.Query, f.active, searchexec.BoolLit[bool])
	name := searchexec.NewIn(q.Query, f.name, searchexec.StringLit[string])
	nameFold := searchexec.NewInFold[string](q.Query, f.name)
	perm := searchexec.NewExists(q.Query, f.perms, f.correlation, f.permName, searchexec.StringLit[string])
	note := searchexec.NewPresence(q.Query, f.note)
	ranked := searchexec.NewFlag(q.Query, f.score.GT(Int(1)))
	text := searchexec.NewText(q.Query, f.name, f.note)

	// SQLite's LOWER folds ASCII letters only, so Ö does not find ö there.
	wörld := []int64{3}
	if s.cfg.Flavor == SQLite {
		wörld = []int64{}
	}
	cases := map[string]struct {
		q    sfSearch
		want []int64
	}{
		"Eq keeps":       {kind.Eq("x"), []int64{1, 4}},
		"Eq drops":       {kind.Eq("none"), []int64{}},
		"EqIn In":        {kind.In("x", "z", "none"), []int64{1, 3, 4}},
		"In int":         {n.In(2, 5), []int64{2, 5}},
		"In int drops":   {n.In(9), []int64{}},
		"In uuid":        {uid.In(sfUID(3), sfUID(101)), []int64{3}},
		"In uuid drops":  {uid.In(uuid.New()), []int64{}},
		"Eq bool true":   {active.Eq(true), []int64{1, 3, 5}},
		"Eq bool false":  {active.Eq(false), []int64{2, 4}},
		"In is exact":    {name.In("istanbul"), []int64{2}},
		"In of no value": {name.In(), []int64{}},
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
		"Contains":                       {text.Contains("WÖRLD_"), wörld},
		"Contains, ASCII case":           {text.Contains("wÖRLD_"), wörld},
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
			assert.Equal(t, tc.want, s.ids(t, f, tc.q))
		})
	}
}

// fieldsInFold checks that InFold folds both sides in the database, by the rule of the
// column. Go's strings.ToLower and the database's LOWER differ on the Turkish dotted I under
// COLLATE "C", so a fold in Go would give another answer there. LOWER($1) alone would fold
// the value by the default collation, not the column's, so the C column would not find its
// own rows.
func (s suite) fieldsInFold(t *testing.T, f *sfFixture) {
	columns := map[string]ColumnString{"name": f.name, "name_c": f.nameC}
	inFold := func(expr ColumnString, term string) []int64 {
		return s.ids(t, f, searchexec.NewInFold[string](f.search("org-a").Query, expr).In(term))
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
				assert.Equal(t, s.lowerEquals(t, f, column, term), inFold(expr, term))
			})
		}
	}

	t.Run("ASCII letters fold on every dialect", func(t *testing.T) {
		assert.Equal(t, []int64{5}, inFold(f.name, "pARIS"))
		assert.Equal(t, []int64{5}, inFold(f.nameC, "pARIS"))
	})

	t.Run(`COLLATE "C" folds ASCII letters only`, func(t *testing.T) {
		if s.cfg.Flavor != Postgres {
			t.Skip(`the "C" collation of PostgreSQL`)
		}
		require.Equal(t, "istanbul", strings.ToLower("İSTANBUL"), "Go folds the dotted I")
		assert.Equal(t, []int64{2, 3}, inFold(f.nameC, "istanbul"), "the database does not, under C")
		assert.Equal(t, []int64{1}, inFold(f.nameC, "İSTANBUL"), "the value folds by C too")
	})

	t.Run("Exists fold", func(t *testing.T) {
		got := s.ids(t, f, searchexec.NewExistsFold[string](f.search("org-a").Query, f.perms, f.correlation,
			f.permName).In("WRITE", "READ"))
		assert.Equal(t, []int64{1, 3}, got)
	})

	t.Run("CockroachDB folds by its ILIKE rule", func(t *testing.T) {
		if s.cfg.Flavor != CockroachDB {
			t.Skip("the other dialects compare with LOWER above")
		}
		// CockroachDB's ILIKE and its LOWER do not agree on the Turkish I. InFold follows
		// ILIKE there, as Contains does: the dotless ı matches i, and İ matches only İ.
		assert.Equal(t, []int64{2, 3, 4}, inFold(f.name, "istanbul"))
		assert.Equal(t, []int64{1}, inFold(f.name, "İSTANBUL"))
	})

	t.Run(`Exists fold, a child column in "C"`, func(t *testing.T) {
		exists := func(term string) []int64 {
			return s.ids(t, f, searchexec.NewExistsFold[string](f.search("org-a").Query, f.perms, f.correlation,
				f.permNameC).In(term))
		}
		assert.Equal(t, []int64{4}, exists("İ"))
		if s.cfg.Flavor == Postgres {
			assert.Equal(t, []int64{}, exists("i"), "C does not fold the dotted I")
		}
	})
}

// lowerEquals returns the org-a ids where LOWER(column) = LOWER(term), from a statement
// written by hand. On PostgreSQL the term takes the collation of the column, so both sides
// fold by the same rule. On CockroachDB, whose ILIKE and LOWER differ on the Turkish I, it is
// the ILIKE match of the escaped term.
func (s suite) lowerEquals(t *testing.T, f *sfFixture, column, term string) []int64 {
	t.Helper()
	match := "LOWER(" + column + ") = LOWER(?)"
	switch {
	case s.cfg.Flavor == CockroachDB:
		match = column + " ILIKE ? ESCAPE '!'"
		term = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(term)
	case s.cfg.Flavor == Postgres && column == "name_c":
		match = "LOWER(" + column + `) = LOWER(?::text COLLATE "C")`
	case s.cfg.Flavor == Postgres:
		match = "LOWER(" + column + ") = LOWER(?::text)"
	}
	rows, err := s.cfg.DB.QueryContext(t.Context(), s.sql("SELECT id FROM "+s.qualified(f.itemsName)+
		" WHERE org = 'org-a' AND "+match+" ORDER BY id"), term)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

// fieldsExistsTwoTenants checks Exists on a child table of two tenants: a child row of
// another tenant never makes a row match, and a row of another tenant never shows.
func (s suite) fieldsExistsTwoTenants(t *testing.T, f *sfFixture) {
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
			assert.Equal(t, tc.want, s.ids(t, f, tc.q))
		})
	}
}

func (s suite) fieldsLimits(t *testing.T, f *sfFixture) {
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
		assert.Equal(t, []int64{5}, s.ids(t, f, in.In(names[:search.MaxInValues]...)))
		assert.Equal(t, []int64{5}, s.ids(t, f, inFold.In(names[:search.MaxInValues]...)))
		assert.Equal(t, []int64{}, s.ids(t, f, text.Contains(term)))
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
			_, err := searchexec.Run(t.Context(), s.cfg.DB, f.spec, tc.q.Query, func(r sfRow) int64 { return r.ID })
			require.ErrorIs(t, err, tc.want)
			assert.True(t, search.IsClientError(err))
		})
	}
}
