// This file is Nanostack-only (not in go-jet): the db/searchexec field types (R7) on every
// dialect. See db/UPSTREAM.md.

package searchexeccontract

import (
	"testing"
	"time"

	"github.com/google/uuid"
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

	items, perms                     keyset.Source
	id, n, score, permItem           ColumnInteger
	org, name, nameC, kind, note     ColumnString
	uid                              ColumnString
	permOrg, permName, permNameC     ColumnString
	active                           ColumnBool
	at                               ColumnTimestampz
	spec                             *searchexec.Spec[sfRow, sfSort, string]
	correlation, correlationNoTenant BoolExpression
}

// newSfFixture creates the items and their permissions. name_c is in "C" on PostgreSQL. The
// times are written in UTC: a SQLite column compares the driver's text, which holds the
// zone.
func (s suite) newSfFixture(t *testing.T) *sfFixture {
	t.Helper()

	f := &sfFixture{itemsName: s.name("nanostack_sf_items")}
	permsName := s.name("nanostack_sf_perms")
	s.create(t, map[string]string{
		f.itemsName: `id BIGINT PRIMARY KEY,
			org ` + s.text() + ` NOT NULL,
			name ` + s.text() + ` NOT NULL,
			name_c ` + s.cText() + ` NOT NULL,
			kind ` + s.text() + ` NOT NULL,
			uid ` + s.uuid() + ` NOT NULL,
			n BIGINT NOT NULL,
			active BOOLEAN NOT NULL,
			at ` + s.timestamptz() + ` NOT NULL,
			note ` + s.text() + `,
			score BIGINT`,
		permsName: `org ` + s.text() + ` NOT NULL,
			item_id BIGINT NOT NULL REFERENCES ` + s.qualified(f.itemsName) + ` (id),
			name ` + s.text() + ` NOT NULL,
			name_c ` + s.cText() + ` NOT NULL`,
	}, f.itemsName, permsName)
	for _, r := range sfItems {
		s.exec(t, "INSERT INTO "+s.qualified(f.itemsName)+
			" (id, org, name, name_c, kind, uid, n, active, at, note, score)"+
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			r.id, r.org, r.name, r.name, r.kind, sfUID(r.id).String(), r.n, r.active, r.at.UTC(), r.note, r.score)
	}
	for _, p := range sfPerms {
		s.exec(t, "INSERT INTO "+s.qualified(permsName)+" (org, item_id, name, name_c) VALUES (?, ?, ?, ?)",
			p.org, p.item, p.name, p.name)
	}

	f.id, f.n, f.score = IntegerColumn("id"), IntegerColumn("n"), IntegerColumn("score")
	f.org, f.name, f.nameC = StringColumn("org"), StringColumn("name"), StringColumn("name_c")
	f.kind, f.note, f.uid = StringColumn("kind"), StringColumn("note"), StringColumn("uid")
	f.active, f.at = BoolColumn("active"), TimestampzColumn("at")
	f.items = s.table(f.itemsName, f.id, f.org, f.name, f.nameC, f.kind, f.uid, f.n, f.active, f.at, f.note, f.score)
	f.permOrg, f.permItem = StringColumn("org"), IntegerColumn("item_id")
	f.permName, f.permNameC = StringColumn("name"), StringColumn("name_c")
	f.perms = s.table(permsName, f.permOrg, f.permItem, f.permName, f.permNameC)
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
func (s suite) ids(t *testing.T, f *sfFixture, q sfSearch) []int64 {
	t.Helper()
	result, err := searchexec.Run(t.Context(), s.cfg.DB, f.spec, q.Query, func(r sfRow) int64 { return r.ID })
	require.NoError(t, err)
	require.Empty(t, result.NextCursor, "one page holds every match")
	if result.Items == nil {
		return []int64{}
	}
	return result.Items
}

// The org-a rows, for the tests where a filter keeps every row.
var sfAllA = []int64{1, 2, 3, 4, 5}

func (s suite) runFields(t *testing.T) {
	f := s.newSfFixture(t)
	t.Run("every field, both ways", func(t *testing.T) { s.fieldsBothWays(t, f) })
	t.Run("InFold agrees with the column", func(t *testing.T) { s.fieldsInFold(t, f) })
	t.Run("Exists on a child of two tenants", func(t *testing.T) { s.fieldsExistsTwoTenants(t, f) })
	t.Run("the limits", func(t *testing.T) { s.fieldsLimits(t, f) })
}
