//go:build integration

package torture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/pgerr"
)

// The write tests attack the derived writes of Wides and WShelves from the
// wrong scope. The world has two tenants (orgs[0]/products[0] and
// orgs[1]/products[1]) and two workspaces; a test snapshots the whole table
// with plain SQL, attacks, and reads the table again: only the rows it
// meant to change may differ.

// wctx is a context carrying workspace ws, or none for "".
func wctx(ws string) context.Context {
	if ws == "" {
		return context.Background()
	}
	return workspacescope.With(context.Background(), ws)
}

// newWide is a row with every NOT NULL column set and every nullable one NULL.
func newWide(label, slug string) model.Wide {
	at := time.Date(2026, 3, 4, 10, 30, 0, 123456000, time.UTC)
	return model.Wide{
		ID: idOf(label), Slug: slug,
		CUUID: idOf("c-" + label), CText: "t'x \"q\" %_\\", CVarchar: "v8", CCitext: "Mixed",
		CSmall: -7, CInt: 1 << 30, CBig: 1 << 60, CReal: 0.5, CDouble: 1e-300, CBool: true,
		CMood: model.Mood_NeedsQuote, CInet: "10.1.2.3", CJSON: `{"a": [1, 2]}`, CBytes: []byte{0, 1, 255},
		CDate: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), CTs: at, CTstz: at,
		CTime:   time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC),
		CTimetz: time.Date(0, 1, 1, 9, 0, 0, 0, time.FixedZone("z", 7200)),
		CTexts:  []string{"a", "b,c", ""}, CInts: []int32{1, -2},
	}
}

// fullWide sets every nullable column of w.
func fullWide(w model.Wide) model.Wide {
	at := time.Date(2027, 1, 2, 3, 4, 5, 6000, time.UTC)
	u, s, i16, i32, i64 := idOf("n-"+w.Slug), "n't", int16(9), int32(-9), int64(99)
	f32, f64, b, mood, inet := float32(1.5), 2.5, false, model.Mood_Sad, "::1"
	js, by := `{"n": null}`, []byte{9}
	date, tm, tmz := at.Truncate(
		24*time.Hour,
	), time.Date(
		0,
		1,
		1,
		1,
		2,
		3,
		0,
		time.UTC,
	), time.Date(
		0,
		1,
		1,
		1,
		2,
		3,
		0,
		time.FixedZone("z", -3600),
	)
	texts, ints := []string{"x"}, []int32{7}
	w.NUUID, w.NText, w.NVarchar, w.NCitext, w.NSmall, w.NInt, w.NBig = &u, &s, &s, &s, &i16, &i32, &i64
	w.NReal, w.NDouble, w.NBool, w.NMood, w.NInet, w.NJSON, w.NBytes = &f32, &f64, &b, &mood, &inet, &js, &by
	w.NDate, w.NTs, w.NTstz, w.NTime, w.NTimetz = &date, &at, &at, &tm, &tmz
	nt, ni := pq.StringArray(texts), pq.Int32Array(ints)
	w.NTexts, w.NInts = &nt, &ni
	return w
}

// clearWide sets every nullable column of w to NULL.
func clearWide(w model.Wide) model.Wide {
	n := newWide("", "")
	w.NUUID, w.NText, w.NVarchar, w.NCitext, w.NSmall, w.NInt, w.NBig = n.NUUID, n.NText, n.NVarchar, n.NCitext, n.NSmall, n.NInt, n.NBig
	w.NReal, w.NDouble, w.NBool, w.NMood, w.NInet, w.NJSON, w.NBytes = n.NReal, n.NDouble, n.NBool, n.NMood, n.NInet, n.NJSON, n.NBytes
	w.NDate, w.NTs, w.NTstz, w.NTime, w.NTimetz, w.NTexts, w.NInts = n.NDate, n.NTs, n.NTstz, n.NTime, n.NTimetz, n.NTexts, n.NInts
	return w
}

// norm renders w as text that ignores what a round trip may change: the
// zone a time comes back in, and the columns the database writes.
func norm(v any) string {
	rv := reflect.ValueOf(v)
	out := ""
	for i := range rv.NumField() {
		name := rv.Type().Field(i).Name
		if name == "CreatedAt" || name == "UpdatedAt" {
			continue
		}
		out += name + "=" + normValue(rv.Field(i)) + ";"
	}
	return out
}

func normValue(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "NULL"
		}
		return normValue(v.Elem())
	}
	if tm, ok := reflect.TypeAssert[time.Time](v); ok {
		return tm.UTC().Format("0006-01-02T15:04:05.999999999Z")
	}
	return fmt.Sprintf("%v", v.Interface())
}

// snapshot reads every row of table as plain SQL: its id and its JSON text.
func snapshot(t *testing.T, db *sql.DB, table string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT id::text, to_jsonb(t)::text FROM `+table+` t`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, doc string
		if err = rows.Scan(&id, &doc); err != nil {
			t.Fatal(err)
		}
		out[id] = doc
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// diffRows returns the ids whose row differs between two snapshots, or exists in one only.
func diffRows(before, after map[string]string) []string {
	var out []string
	for id, doc := range before {
		if got, ok := after[id]; !ok || got != doc {
			out = append(out, id)
		}
	}
	for id := range after {
		if _, ok := before[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

func wantOnly(t *testing.T, what string, before, after map[string]string, ids ...uuid.UUID) {
	t.Helper()
	want := map[string]bool{}
	for _, id := range ids {
		want[id.String()] = true
	}
	for _, id := range diffRows(before, after) {
		if !want[id] {
			t.Errorf("%s changed the row %s of another scope:\nbefore %s\nafter  %s", what, id, before[id], after[id])
		}
		delete(want, id)
	}
	for id := range want {
		t.Errorf("%s did not change the row %s it meant to change", what, id)
	}
}

// world of wides: a is tenant A in w1, b is tenant B in w1, c is tenant A in w2.
type wideWorld struct {
	db      *sql.DB
	repo    Wides
	a, b, c model.Wide
}

func seedWides(t *testing.T) wideWorld {
	t.Helper()
	db := openTorture(t)
	w := wideWorld{db: db, repo: NewWides(db)}
	mk := func(org, prod int, ws, label, slug string) model.Wide {
		got, err := w.repo.CreateInOrgAndProductAndWorkspace(wctx(ws), orgs[org], products[prod], newWide(label, slug))
		if err != nil {
			t.Fatalf("seed %s: %v", label, err)
		}
		return got
	}
	w.a, w.b = mk(0, 0, "w1", "a", "shared"), mk(1, 1, "w1", "b", "shared")
	w.c = mk(0, 0, "w2", "c", "in-w2")
	return w
}

// Create takes the scope from its parameters and the context: a value that
// carries another tenant's ids and workspace is overridden, an id that
// another tenant owns is a unique violation that leaves its row alone, and
// a context without a workspace creates nothing.
func TestWriteCreateScopeComesFromParameters(t *testing.T) {
	w := seedWides(t)
	ctx := wctx("w1")
	before := snapshot(t, w.db, "wide")

	v := newWide("forged", "forged")
	v.OrgID, v.ProductID, v.WorkspaceID = orgs[1], products[1], "w2"
	got, err := w.repo.CreateInOrgAndProductAndWorkspace(ctx, orgs[0], products[0], v)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != orgs[0] || got.ProductID != products[0] || got.WorkspaceID != "w1" {
		t.Errorf(
			"created in (%s, %s, %s), want the parameters and the context",
			got.OrgID,
			got.ProductID,
			got.WorkspaceID,
		)
	}
	after := snapshot(t, w.db, "wide")
	wantOnly(t, "create", before, after, v.ID)
	if norm(got) != norm(withScope(v, orgs[0], products[0], "w1")) {
		t.Errorf("round trip:\n got %s\nwant %s", norm(got), norm(withScope(v, orgs[0], products[0], "w1")))
	}

	// B's id under A: the primary key refuses, B's row is untouched.
	steal := newWide("steal", "steal")
	steal.ID = w.b.ID
	_, err = w.repo.CreateInOrgAndProductAndWorkspace(ctx, orgs[0], products[0], steal)
	if !pgerr.IsUniqueViolation(err, "wide_pkey") {
		t.Errorf("create with another tenant's id: %v, want a unique violation of wide_pkey", err)
	}
	wantOnly(t, "create over another tenant's id", after, snapshot(t, w.db, "wide"))

	// No workspace in ctx: fails closed, nothing is inserted.
	_, err = w.repo.CreateInOrgAndProductAndWorkspace(
		context.Background(),
		orgs[0],
		products[0],
		newWide("nows", "nows"),
	)
	if !errors.Is(err, nanorepo.ErrNoContextScope) {
		t.Errorf("create without a workspace: %v, want nanorepo.ErrNoContextScope", err)
	}
	wantOnly(t, "create without a workspace", after, snapshot(t, w.db, "wide"))
}

func withScope(v model.Wide, org, prod uuid.UUID, ws string) model.Wide {
	v.OrgID, v.ProductID, v.WorkspaceID = org, prod, ws
	return v
}

// An update from the wrong scope changes no row, answers its documented
// no-row result, and cannot move the row it does reach to another scope.
func TestWriteUpdateWrongScope(t *testing.T) {
	w := seedWides(t)
	before := snapshot(t, w.db, "wide")
	for _, at := range []struct {
		name      string
		org, prod int
		ws        string
	}{
		{"other org", 1, 0, "w1"}, {"other product", 0, 1, "w1"}, {"other workspace", 0, 0, "w2"},
		{"other tenant", 1, 1, "w1"}, {"unknown tenant", 2, 2, "w1"}, {"unknown workspace", 0, 0, "wX"},
	} {
		v := w.a
		v.CText, v.Slug = "hijacked", "hijacked"
		v.OrgID, v.ProductID, v.WorkspaceID = orgs[at.org], products[at.prod], at.ws // a claim in the value
		ctx := wctx(at.ws)
		if _, err := w.repo.UpdateByOrgAndProductAndWorkspaceAndID(
			ctx,
			orgs[at.org],
			products[at.prod],
			v,
		); !errors.Is(
			err,
			sql.ErrNoRows,
		) {
			t.Errorf("%s: update by id: %v, want sql.ErrNoRows", at.name, err)
		}
		opt, err := w.repo.UpdateByOrgAndProductAndWorkspaceAndSlug(ctx, orgs[at.org], products[at.prod], v)
		if err != nil || opt.IsPresent() {
			t.Errorf("%s: update by slug: (%v, %v), want absent", at.name, opt, err)
		}
	}
	wantOnly(t, "an update of the wrong scope", before, snapshot(t, w.db, "wide"))

	// The right scope changes that row only, and its scope columns stay.
	v := w.a
	v.CText, v.OrgID, v.ProductID, v.WorkspaceID = "changed", orgs[1], products[1], "w2"
	got, err := w.repo.UpdateByOrgAndProductAndWorkspaceAndID(wctx("w1"), orgs[0], products[0], v)
	if err != nil {
		t.Fatal(err)
	}
	if got.CText != "changed" || got.OrgID != orgs[0] || got.ProductID != products[0] || got.WorkspaceID != "w1" {
		t.Errorf("updated row %+v: want text changed and the scope kept", got)
	}
	wantOnly(t, "an update", before, snapshot(t, w.db, "wide"), w.a.ID)
}

// A request without a workspace writes nothing: workspace_id has a Present
// function, so a derived create, update or upsert returns
// nanorepo.ErrNoContextScope before any SQL (the repository here sits on a
// closed database: any statement would fail with another error). A read
// keeps the registered filter and takes the whole tenant.
func TestWriteWithoutWorkspaceFailsClosed(t *testing.T) {
	w := seedWides(t)
	before := snapshot(t, w.db, "wide")
	dead := openTorture(t)
	if err := dead.Close(); err != nil {
		t.Fatal(err)
	}
	repo := NewWides(dead)
	bare := context.Background()
	v := w.c // tenant A, workspace w2
	v.CText = "from no workspace"
	if _, err := repo.UpdateByOrgAndProductAndWorkspaceAndID(
		bare,
		orgs[0],
		products[0],
		v,
	); !errors.Is(
		err,
		nanorepo.ErrNoContextScope,
	) {
		t.Errorf("update: %v, want ErrNoContextScope", err)
	}
	if _, err := repo.UpdateByOrgAndProductAndWorkspaceAndSlug(
		bare,
		orgs[0],
		products[0],
		v,
	); !errors.Is(
		err,
		nanorepo.ErrNoContextScope,
	) {
		t.Errorf("update by slug: %v, want ErrNoContextScope", err)
	}
	if _, err := repo.UpsertByOrgAndProductAndWorkspaceAndSlug(
		bare,
		orgs[0],
		products[0],
		v,
	); !errors.Is(
		err,
		nanorepo.ErrNoContextScope,
	) {
		t.Errorf("upsert: %v, want ErrNoContextScope", err)
	}
	if _, err := repo.CreateInOrgAndProductAndWorkspace(
		bare,
		orgs[0],
		products[0],
		newWide("x", "x"),
	); !errors.Is(
		err,
		nanorepo.ErrNoContextScope,
	) {
		t.Errorf("create: %v, want ErrNoContextScope", err)
	}
	wantOnly(t, "a write without a workspace", before, snapshot(t, w.db, "wide"))
	// A read on the same bare context takes the whole tenant, and no other.
	got, err := w.repo.FindByOrgAndProductAndWorkspaceAndID(bare, orgs[0], products[0], w.c.ID)
	if err != nil || !got.IsPresent() {
		t.Errorf("read without a workspace: %v %v, want the tenant's row", got, err)
	}
}
