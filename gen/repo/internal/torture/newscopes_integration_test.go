//go:build integration

package torture

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/regionscope"
)

// A nullable tenant column that is a hard scope must be named, with equality:
// a row with no org matches no org, and a read may end in Internal.
func TestNullableHardScope(t *testing.T) {
	db := openTorture(t)
	ctx, docs := context.Background(), NewSharedDocs(db)
	insert := func() {
		for id, org := range map[string]any{"d1": orgs[0], "d2": orgs[1], "d3": nil, "d4": orgs[0]} {
			title := "t"
			if id == "d4" {
				title = "u"
			}
			mustExec(t, db, `INSERT INTO shared_doc (id, org_id, title) VALUES ($1,$2,$3)`, id, org, title)
		}
	}
	insert()
	for id, found := range map[string]bool{"d1": true, "d2": false, "d3": false} {
		if o, err := docs.FindByOrgAndID(ctx, orgs[0], id); err != nil || o.IsPresent() != found {
			t.Errorf("FindByOrgAndID(org 0, %s): %v %v, want present %v", id, o, err, found)
		}
	}
	if rows, err := docs.FindByOrgAndTitle(ctx, orgs[0], "t"); err != nil || len(rows) != 1 {
		t.Errorf("FindByOrgAndTitle: %d %v", len(rows), err)
	}
	if rows, err := docs.FindByOrg(ctx, orgs[2]); err != nil || len(rows) != 0 {
		t.Errorf("FindByOrg of an org with no row: %d %v", len(rows), err)
	}
	if n, err := docs.CountByOrg(ctx, orgs[0]); err != nil || n != 2 {
		t.Errorf("CountByOrg: %d %v", n, err)
	}
	// Internal reads see the row with no org: that is what the name says.
	if o, err := docs.FindByIDInternal(ctx, "d3"); err != nil || !o.IsPresent() {
		t.Errorf("FindByIDInternal d3: %v %v", o, err)
	}
	if rows, err := docs.FindAllInternal(ctx); err != nil || len(rows) != 4 {
		t.Errorf("FindAllInternal: %d %v", len(rows), err)
	}
	// A delete reaches only its org: a row with no org is out of reach of every org.
	if ok, err := docs.DeleteByOrgAndID(ctx, orgs[1], "d1"); err != nil || ok {
		t.Errorf("delete d1 as org 1: %v %v", ok, err)
	}
	if ok, err := docs.DeleteByOrgAndID(ctx, orgs[0], "d3"); err != nil || ok {
		t.Errorf("delete d3, which has no org, as org 0: %v %v", ok, err)
	}
	if n, err := docs.DeleteByOrgIDAndTitle(ctx, orgs[0], "t"); err != nil || n != 1 {
		t.Errorf("DeleteByOrgIDAndTitle: %d %v", n, err)
	}
	if left := column(t, db, `SELECT id FROM shared_doc`); !slices.Equal(left, []string{"d2", "d3", "d4"}) {
		t.Errorf("left %v, want d2 d3 d4", left)
	}
}

// Two context scopes and a hard one: a name gives all three, each context
// scope applies its own function, and a missing one keeps every row of its
// column. The workspace in its field form is a parameter, not ctx.
func TestTwoContextScopes(t *testing.T) {
	db := openTorture(t)
	type zone struct {
		id, ws, region string
		org            int
	}
	var rows []zone
	for o := range 2 {
		for _, ws := range []string{"w1", "w2", ""} {
			for _, region := range []string{"r1", "r2"} {
				z := zone{id: "z-" + string(rune('a'+o)) + ws + region, ws: ws, region: region, org: o}
				var wsArg any
				if ws != "" {
					wsArg = ws
				}
				mustExec(
					t,
					db,
					`INSERT INTO zone (id, org_id, workspace_id, region_id, name) VALUES ($1,$2,$3,$4,'same')`,
					z.id,
					orgs[o],
					wsArg,
					region,
				)
				rows = append(rows, z)
			}
		}
	}
	repo := NewZones(db)
	expect := func(org int, ws, region string) []string {
		var out []string
		for _, z := range rows {
			if z.org == org && (ws == "" || z.ws == ws) && (region == "" || z.region == region) {
				out = append(out, z.id)
			}
		}
		slices.Sort(out)
		return out
	}
	for o := range 3 {
		for _, ws := range ctxValues {
			for _, region := range []string{"", "r1", "r2", "rX"} {
				ctx := ctxOf(ws)
				if region != "" {
					ctx = regionscope.With(ctx, region)
				}
				var want []string
				if o < 2 {
					want = expect(o, ws, region)
				}
				got, err := repo.FindByOrgAndRegionAndWorkspaceAndName(ctx, orgs[o], "same")
				var ids []string
				for _, z := range got {
					ids = append(ids, z.ID)
				}
				slices.Sort(ids)
				if err != nil || !slices.Equal(ids, want) && (len(ids) != 0 || len(want) != 0) {
					t.Fatalf("org %d ws %q region %q: %v %v, want %v", o, ws, region, ids, err, want)
				}
				if n, cErr := repo.CountByOrgAndWorkspaceAndRegion(ctx, orgs[o]); cErr != nil || n != int64(len(want)) {
					t.Fatalf("count org %d ws %q region %q: %d %v, want %d", o, ws, region, n, cErr, len(want))
				}
				tx, err := db.BeginTx(context.Background(), nil)
				if err != nil {
					t.Fatal(err)
				}
				n, err := NewZones(tx).DeleteByOrgAndWorkspaceAndRegionAndName(ctx, orgs[o], "same")
				left := column(t, db, `SELECT id FROM zone`) // outside the transaction: still every row
				if len(left) != len(rows) {
					t.Fatalf("a delete in an open transaction is visible outside it")
				}
				after := column(t, tx, `SELECT id FROM zone`)
				_ = tx.Rollback()
				if err != nil || n != int64(len(want)) || len(after) != len(rows)-len(want) {
					t.Fatalf(
						"delete org %d ws %q region %q: %d %v, %d rows left; want %d deleted",
						o,
						ws,
						region,
						n,
						err,
						len(after),
						len(want),
					)
				}
				for _, id := range want {
					if slices.Contains(after, id) {
						t.Fatalf("delete left %s", id)
					}
				}
			}
		}
	}
	// The field form takes the workspace from a parameter and ignores ctx for it.
	got, err := repo.FindByOrgAndWorkspaceIDAndRegionAndName(ctxOf("w2"), orgs[0], "w1", "same")
	if err != nil || len(got) != 2 {
		t.Errorf("WorkspaceID form: %d rows %v, want the 2 regions of w1", len(got), err)
	}
	// Internal skips the context scopes: all rows of the org.
	if got, err = repo.FindByOrgAndNameInternal(ctxOf("w1"), orgs[0], "same"); err != nil || len(got) != 6 {
		t.Errorf("Internal: %d %v, want 6", len(got), err)
	}
}

// A NOT NULL and a nullable hard scope on one table: a name gives both, and
// a row with no product matches no product.
func TestMixedHardScopes(t *testing.T) {
	db := openTorture(t)
	for o := range 2 {
		for p, prod := range []any{products[0], products[1], nil} {
			mustExec(t, db, `INSERT INTO mixed (id, org_id, product_id, name) VALUES ($1,$2,$3,'same')`,
				"m"+string(rune('0'+o))+string(rune('0'+p)), orgs[o], prod)
		}
	}
	repo := NewMixeds(db)
	for o := range 3 {
		for p := range 3 {
			got, err := repo.FindByOrgAndProductAndName(context.Background(), orgs[o], products[p], "same")
			want := 0
			if o < 2 && p < 2 {
				want = 1
			}
			if err != nil || len(got) != want {
				t.Fatalf("org %d product %d: %d rows %v, want %d", o, p, len(got), err, want)
			}
			if n, cErr := repo.CountByOrgAndProduct(
				context.Background(),
				orgs[o],
				products[p],
			); cErr != nil ||
				n != int64(want) {
				t.Fatalf("count org %d product %d: %d %v", o, p, n, cErr)
			}
		}
	}
	if n, err := repo.DeleteByOrgAndProductAndName(
		context.Background(),
		orgs[0],
		products[0],
		"same",
	); err != nil ||
		n != 1 {
		t.Fatalf("delete: %d %v", n, err)
	}
	if left := column(
		t,
		db,
		`SELECT id FROM mixed`,
	); !slices.Equal(
		left,
		[]string{"m00" + "", "m01", "m02", "m10", "m11", "m12"}[1:],
	) {
		t.Errorf("left %v: only m00 goes", left)
	}
	// Internal skips the nullable product: the NULL row is read.
	if got, err := repo.FindByOrgAndNameInternal(context.Background(), orgs[1], "same"); err != nil || len(got) != 3 {
		t.Errorf("FindByOrgAndNameInternal: %d %v, want 3 with the NULL-product row", len(got), err)
	}
}

// ScopeImplies: a scope column that a name gives stands for the ones it
// implies, and the filter that runs is the one the name says.
func TestScopeImplies(t *testing.T) {
	db := openTorture(t)
	// Consistent data: each site belongs to one product, each product to one org.
	for o := range 2 {
		for p := range 2 {
			prod := uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(o), byte(p)})
			mustExec(t, db, `INSERT INTO shelf (id, org_id, product_id, name) VALUES ($1,$2,$3,'same')`,
				fmt.Sprintf("shelf-%d%d", o, p), orgs[o], prod)
			for s := range 2 {
				site := uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(o), byte(p), byte(s)})
				mustExec(t, db, `INSERT INTO bin (id, org_id, product_id, site_id, name) VALUES ($1,$2,$3,$4,'same')`,
					fmt.Sprintf("bin-%d%d%d", o, p, s), orgs[o], prod, site)
			}
		}
	}
	// A row is found through its own site and through no other.
	bins, shelves := NewBins(db), NewShelves(db)
	ctx := context.Background()
	site := uuid.NewSHA1(uuid.NameSpaceOID, []byte{1, 0, 1})
	if o, err := bins.FindBySiteAndID(ctx, site, "bin-101"); err != nil || !o.IsPresent() {
		t.Errorf("FindBySiteAndID: %v %v", o, err)
	}
	for _, other := range []uuid.UUID{uuid.New(), uuid.NewSHA1(uuid.NameSpaceOID, []byte{1, 0, 0}), uuid.NewSHA1(uuid.NameSpaceOID, []byte{0, 0, 1})} {
		if o, err := bins.FindBySiteAndID(ctx, other, "bin-101"); err != nil || o.IsPresent() {
			t.Errorf("FindBySiteAndID of another site: %v %v", o, err)
		}
	}
	if n, err := bins.CountBySite(ctx, site); err != nil || n != 1 {
		t.Errorf("CountBySite: %d %v", n, err)
	}
	if n, err := bins.DeleteBySiteAndName(ctx, site, "same"); err != nil || n != 1 {
		t.Errorf("DeleteBySiteAndName: %d %v, want only that site", n, err)
	}
	if n := len(column(t, db, `SELECT id FROM bin`)); n != 7 {
		t.Errorf("%d bins left, want 7", n)
	}
	product := uuid.NewSHA1(uuid.NameSpaceOID, []byte{0, 1})
	if o, err := shelves.FindByProductAndID(ctx, product, "shelf-01"); err != nil || !o.IsPresent() {
		t.Errorf("FindByProductAndID: %v %v", o, err)
	}
	if o, err := shelves.FindByProductAndID(ctx, product, "shelf-11"); err != nil || o.IsPresent() {
		t.Errorf("FindByProductAndID of another product's shelf: %v %v", o, err)
	}
	if n, err := shelves.DeleteByProductAndName(ctx, product, "same"); err != nil || n != 1 {
		t.Errorf("DeleteByProductAndName: %d %v", n, err)
	}
	if n := len(column(t, db, `SELECT id FROM shelf`)); n != 3 {
		t.Errorf("%d shelves left, want 3", n)
	}
	// The three named: the same rows as the narrowest.
	rows, err := bins.FindByOrgAndProductAndSiteAndName(ctx, orgs[1], uuid.NewSHA1(uuid.NameSpaceOID, []byte{1, 1}),
		uuid.NewSHA1(uuid.NameSpaceOID, []byte{1, 1, 0}), "same")
	if err != nil || len(rows) != 1 {
		t.Errorf("FindByOrgAndProductAndSiteAndName: %d %v", len(rows), err)
	}
}

// A join table with the tenant column in its composite key.
func TestJoinTableIsolation(t *testing.T) {
	db := openTorture(t)
	user := idOf("user")
	for o := range 2 {
		for g := int64(1); g <= 2; g++ {
			mustExec(
				t,
				db,
				`INSERT INTO member (org_id, group_id, user_id, role) VALUES ($1,$2,$3,'r')`,
				orgs[o],
				g,
				user,
			)
		}
	}
	repo, ctx := NewMembers(db), context.Background()
	for o := range 3 {
		want := 0
		if o < 2 {
			want = 1
		}
		if m, err := repo.FindByOrgAndGroupIDAndUserID(
			ctx,
			orgs[o],
			1,
			user,
		); err != nil ||
			m.IsPresent() != (want == 1) {
			t.Errorf("org %d: %v %v", o, m, err)
		}
		if rows, err := repo.FindByOrgAndUserID(ctx, orgs[o], user); err != nil || len(rows) != 2*want {
			t.Errorf("org %d FindByOrgAndUserID: %d %v", o, len(rows), err)
		}
		if n, err := repo.CountByOrgAndGroupID(ctx, orgs[o], 1); err != nil || n != int64(want) {
			t.Errorf("org %d count: %d %v", o, n, err)
		}
	}
	if ok, err := repo.DeleteByOrgAndGroupIDAndUserID(ctx, orgs[1], 1, user); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if n, err := repo.DeleteByOrgAndGroupID(ctx, orgs[0], 2); err != nil || n != 1 {
		t.Fatalf("DeleteByOrgAndGroupID: %d %v", n, err)
	}
	if left := column(t, db, `SELECT org_id::text||'/'||group_id FROM member`); len(left) != 2 {
		t.Errorf("left %v, want org0/1 and org1/2", left)
	} else if !slices.Contains(left, orgs[0].String()+"/1") || !slices.Contains(left, orgs[1].String()+"/2") {
		t.Errorf("left %v, want org0/1 and org1/2", left)
	}
}
