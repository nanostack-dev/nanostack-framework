//go:build integration

package torture

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
)

// An upsert cannot take another scope's row through ON CONFLICT: wide
// conflicts on (org_id, product_id, slug) and guards the workspace in its DO
// UPDATE; wshelf conflicts on (product_id, slug) alone, so a tenant that
// owns the same product id meets the row of another tenant and the guard on
// org_id and workspace_id refuses it.
func TestWriteUpsertCannotHijackARow(t *testing.T) {
	w := seedWides(t)
	before := snapshot(t, w.db, "wide")
	// Another workspace of the tenant, same slug as a's: the key conflicts, the guard refuses.
	v := newWide("u1", w.a.Slug)
	v.CText = "hijack by workspace"
	if _, err := w.repo.UpsertByOrgAndProductAndWorkspaceAndSlug(
		wctx("wX"),
		orgs[0],
		products[0],
		v,
	); !errors.Is(
		err,
		sql.ErrNoRows,
	) {
		t.Errorf("upsert of a row of another workspace: %v, want sql.ErrNoRows", err)
	}
	wantOnly(t, "an upsert from another workspace", before, snapshot(t, w.db, "wide"))
	// The right workspace updates the row it owns, and keeps its id and scope.
	got, err := w.repo.UpsertByOrgAndProductAndWorkspaceAndSlug(wctx("w1"), orgs[0], products[0], v)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != w.a.ID || got.CText != "hijack by workspace" || got.WorkspaceID != "w1" || got.OrgID != orgs[0] {
		t.Errorf("upsert of the owner: %+v", got)
	}
	wantOnly(t, "an upsert by the owner", before, snapshot(t, w.db, "wide"), w.a.ID)
	// Another tenant with the same slug has its own key: it inserts, and a is unchanged.
	mid := snapshot(t, w.db, "wide")
	v2 := newWide("u2", w.a.Slug)
	got, err = w.repo.UpsertByOrgAndProductAndWorkspaceAndSlug(wctx("w1"), orgs[1], products[0], v2)
	if err != nil || got.OrgID != orgs[1] {
		t.Fatalf("upsert of another org: %v, %+v", err, got)
	}
	wantOnly(t, "an upsert of another org", mid, snapshot(t, w.db, "wide"), v2.ID)
	// No workspace: the stamp fails first.
	if _, err = w.repo.UpsertByOrgAndProductAndWorkspaceAndSlug(
		context.Background(),
		orgs[0],
		products[0],
		v,
	); !errors.Is(
		err,
		nanorepo.ErrNoContextScope,
	) {
		t.Errorf("upsert without a workspace: %v, want ErrNoContextScope", err)
	}
}

func TestWriteUpsertOnAKeyWithoutTheTenant(t *testing.T) {
	db := openTorture(t)
	repo := NewWShelves(db)
	shelf := func(id, slug, body string) model.Wshelf { return model.Wshelf{ID: id, Slug: slug, Body: body} }
	if err := repo.CreateInOrgAndProductAndWorkspace(
		wctx("w1"),
		orgs[0],
		products[0],
		shelf("a", "s", "a"),
	); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateInOrgAndProductAndWorkspace(
		wctx("w1"),
		orgs[1],
		products[1],
		shelf("b", "s", "b"),
	); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, db, "wshelf")
	// Org B claims A's product id: the key (product_id, slug) conflicts with A's row.
	for _, at := range []struct {
		name string
		org  int
		ws   string
	}{{"another org, same product", 1, "w1"}, {"same org, another workspace", 0, "w2"}} {
		err := repo.UpsertByOrgAndProductAndWorkspaceAndSlug(
			wctx(at.ws),
			orgs[at.org],
			products[0],
			shelf("x", "s", "taken over by "+at.name),
		)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s: %v, want sql.ErrNoRows", at.name, err)
		}
	}
	wantOnlyText(t, "an upsert through the key of another scope", before, snapshot(t, db, "wshelf"))
	// The owner updates its row; the id of the value never replaces the stored id.
	if err := repo.UpsertByOrgAndProductAndWorkspaceAndSlug(
		wctx("w1"),
		orgs[0],
		products[0],
		shelf("zzz", "s", "owner"),
	); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, db, "wshelf")
	if len(diffRows(before, after)) != 1 || before["b"] != after["b"] {
		t.Errorf("owner upsert changed %v", diffRows(before, after))
	}
	got, err := repo.FindByOrgAndProductAndWorkspaceAndID(wctx("w1"), orgs[0], products[0], "a")
	if err != nil || !got.IsPresent() || got.Value().Body != "owner" {
		t.Errorf("row a after the owner's upsert: %+v, %v", got, err)
	}
	// An update cannot move a row to another product either: the scope columns are never SET.
	if err = repo.UpdateByOrgAndProductAndWorkspaceAndID(
		wctx("w1"),
		orgs[1],
		products[1],
		model.Wshelf{ID: "a", Slug: "z", Body: "z"},
	); !errors.Is(
		err,
		sql.ErrNoRows,
	) {
		t.Errorf("update of A's row by B: %v, want sql.ErrNoRows", err)
	}
}

// wantOnlyText fails when two snapshots differ.
func wantOnlyText(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if d := diffRows(before, after); len(d) > 0 {
		t.Errorf("%s changed the rows %v", what, d)
	}
}

// Every column type, NOT NULL and nullable, round-trips through Create,
// Update and Upsert, and a nullable column goes from NULL to a value and back.
func TestWriteTypesRoundTrip(t *testing.T) {
	db := openTorture(t)
	repo := NewWides(db)
	ctx := wctx("w1")
	org, prod := orgs[0], products[0]
	want := func(what string, input, got model.Wide) {
		t.Helper()
		input.OrgID, input.ProductID, input.WorkspaceID = org, prod, "w1"
		if norm(input) != norm(got) {
			t.Errorf("%s:\n in  %s\n out %s", what, norm(input), norm(got))
		}
		found, err := repo.FindByOrgAndProductAndWorkspaceAndID(ctx, org, prod, got.ID)
		if err != nil || !found.IsPresent() || norm(found.Value()) != norm(got) {
			t.Errorf("%s: read back %v, %v", what, found, err)
		}
	}
	// Create: nullable NULL, then nullable set.
	empty, full := newWide("empty", "empty"), fullWide(newWide("full", "full"))
	for _, in := range []model.Wide{empty, full} {
		got, err := repo.CreateInOrgAndProductAndWorkspace(ctx, org, prod, in)
		if err != nil {
			t.Fatalf("create %s: %v", in.Slug, err)
		}
		want("create "+in.Slug, in, got)
	}
	// Update by id and by slug: NULL -> value, value -> NULL.
	set := fullWide(empty)
	set.CText, set.CMood, set.CBytes = "updated", model.Mood_Happy, []byte{}
	got, err := repo.UpdateByOrgAndProductAndWorkspaceAndID(ctx, org, prod, set)
	if err != nil {
		t.Fatal(err)
	}
	want("update null -> value", set, got)
	cleared := clearWide(full)
	cleared.CJSON = `{}`
	opt, err := repo.UpdateByOrgAndProductAndWorkspaceAndSlug(ctx, org, prod, cleared)
	if err != nil || !opt.IsPresent() {
		t.Fatalf("update value -> null: %v, %v", opt, err)
	}
	want("update value -> null", cleared, opt.Value())
	// Upsert: insert a new slug with every nullable set, then update it to NULL.
	up := fullWide(newWide("up", "up"))
	got, err = repo.UpsertByOrgAndProductAndWorkspaceAndSlug(ctx, org, prod, up)
	if err != nil {
		t.Fatal(err)
	}
	want("upsert insert", up, got)
	up2 := clearWide(up)
	up2.CInts, up2.CTexts = []int32{}, []string{}
	got, err = repo.UpsertByOrgAndProductAndWorkspaceAndSlug(ctx, org, prod, up2)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != up.ID {
		t.Errorf("upsert changed the id: %s -> %s", up.ID, got.ID)
	}
	want("upsert null", up2, got)
}
