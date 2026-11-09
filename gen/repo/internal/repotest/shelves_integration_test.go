//go:build integration

package repotest

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
)

// These tests attack the derived writes of shelves.go, a table with two hard
// scopes (tenant_id, store_id) and a context scope, whose key (store_id,
// name) holds one hard scope only.

func shelfRows(t *testing.T, f *fixture) string {
	t.Helper()
	res, err := f.db.QueryContext(context.Background(),
		`SELECT id, tenant_id = $1, store_id, workspace_id, name, capacity FROM shelves ORDER BY id`, f.tenant)
	if err != nil {
		t.Fatalf("read shelves: %v", err)
	}
	defer res.Close()
	out := ""
	for res.Next() {
		var id, store, ws, name string
		var mine bool
		var capacity int32
		if err = res.Scan(&id, &mine, &store, &ws, &name, &capacity); err != nil {
			t.Fatalf("scan: %v", err)
		}
		owner := "B"
		if mine {
			owner = "A"
		}
		out += id + " " + owner + " " + store + " " + ws + " " + name + " " + string(rune('0'+capacity)) + "\n"
	}
	return out
}

// Each hard scope of the name overwrites the value's column: a value that
// names the other tenant and another store lands in the caller's. An update
// or an upsert from the other tenant, on the same store and name, changes
// nothing: the update filters on both hard scopes, and the upsert, whose key
// holds the store only, updates only when the existing row has the caller's
// tenant and workspace.
func TestShelvesTwoHardScopes(t *testing.T) {
	f := newFixture(t)
	repo := NewShelfRepository(f.db)
	w1 := workspacescope.With(context.Background(), "w1")
	got, err := repo.CreateInTenantAndStoreIDAndWorkspace(w1, f.tenant, "s1",
		model.Shelves{ID: "a1", TenantID: f.other, StoreID: "s2", WorkspaceID: "w9", Name: "top", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != f.tenant || got.StoreID != "s1" || got.WorkspaceID != "w1" || got.CreatedAt.IsZero() {
		t.Fatalf("created %+v, want the tenant, s1, w1 and a database created_at", got)
	}
	want := "a1 A s1 w1 top 1\n"
	attack := model.Shelves{ID: "a1", TenantID: f.tenant, StoreID: "s1", Name: "top", Capacity: 9}
	if _, err = repo.UpdateByTenantAndStoreIDAndWorkspaceAndID(w1, f.other, "s1", attack); !errors.Is(err,
		sql.ErrNoRows) {
		t.Fatalf("update from the other tenant: %v, want sql.ErrNoRows", err)
	}
	if _, err = repo.UpdateByTenantAndStoreIDAndWorkspaceAndID(w1, f.tenant, "s2", attack); !errors.Is(err,
		sql.ErrNoRows) {
		t.Fatalf("update in another store: %v, want sql.ErrNoRows", err)
	}
	if _, err = repo.UpsertByTenantAndStoreIDAndWorkspaceAndName(w1, f.other, "s1",
		model.Shelves{ID: "b1", Name: "top", Capacity: 9}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("upsert from the other tenant onto the tenant's store and name: %v, want sql.ErrNoRows", err)
	}
	if _, err = repo.UpsertByTenantAndStoreIDAndWorkspaceAndName(workspacescope.With(context.Background(), "w2"),
		f.tenant, "s1", model.Shelves{ID: "a2", Name: "top", Capacity: 9}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("upsert from another workspace: %v, want sql.ErrNoRows", err)
	}
	if got := shelfRows(t, f); got != want {
		t.Fatalf("shelves:\n%swant:\n%s", got, want)
	}
	if got, err = repo.UpsertByTenantAndStoreIDAndWorkspaceAndName(w1, f.tenant, "s1",
		model.Shelves{ID: "new", TenantID: f.other, Name: "top", Capacity: 5}); err != nil || got.ID != "a1" {
		t.Fatalf("upsert in scope: %+v, %v, want a1 updated", got, err)
	}
	if got, err = repo.UpdateByTenantAndStoreIDAndWorkspaceAndID(w1, f.tenant, "s1",
		model.Shelves{ID: "a1", TenantID: f.other, StoreID: "s2", Name: "low", Capacity: 7}); err != nil {
		t.Fatal(err)
	}
	if got := shelfRows(t, f); got != "a1 A s1 w1 low 7\n" {
		t.Fatalf("shelves:\n%swant a1 renamed in its own scope", got)
	}
}
