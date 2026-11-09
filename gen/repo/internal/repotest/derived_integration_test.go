//go:build integration

package repotest

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// These tests run every derived method of ItemRepository on PostgreSQL and
// compare it with the hand-written body a developer would write on the
// base: the same rows, the same counts, the same errors.

// Each derived find returns what the hand-written condition returns, never
// the row of another tenant.
func TestDerivedFindsMatchHandWritten(t *testing.T) {
	f := newFixture(t)
	ctx, r, w := context.Background(), f.repo, f.repo.where
	for _, c := range []struct {
		tenant  uuid.UUID
		sku, id string
	}{{f.tenant, "A-1", "a1"}, {f.other, "A-1", "b1"}, {f.tenant, "zz", "a2"}, {f.tenant, "A-3", "b1"}} {
		derived, err := r.FindByTenantIDAndSku(ctx, c.tenant, c.sku)
		hand, handErr := r.findOne(ctx, w.TenantID(c.tenant).AND(w.Sku(c.sku)))
		sameOption(t, "FindByTenantIDAndSku "+c.sku, derived, err, hand, handErr)
		derived, err = r.FindByTenantIDAndID(ctx, c.tenant, c.id)
		hand, handErr = r.findOne(ctx, w.TenantID(c.tenant).AND(w.ID(c.id)))
		sameOption(t, "FindByTenantIDAndID "+c.id, derived, err, hand, handErr)
	}
	if found, err := r.FindByTenantIDAndID(ctx, f.tenant, "b1"); err != nil || found.IsPresent() {
		t.Fatalf("another tenant's row: got %v, %v", found, err)
	}
	for _, status := range []model.ItemStatus{model.ItemStatus_Active, model.ItemStatus_Archived} {
		derived, err := r.FindByTenantIDAndStatus(ctx, f.tenant, status)
		hand, handErr := r.findAll(ctx, w.TenantID(f.tenant).AND(w.Status(status)))
		if err != nil || handErr != nil || !slices.Equal(sorted(ids(derived)), sorted(ids(hand))) {
			t.Errorf("FindByTenantIDAndStatus %s: got %v, %v; hand-written %v, %v", status, ids(derived), err,
				ids(hand), handErr)
		}
	}
	all, err := r.FindAllInternal(ctx)
	if err != nil || !slices.Equal(sorted(ids(all)), []string{"a1", "a2", "a3", "b1"}) {
		t.Fatalf("FindAllInternal: got %v, %v", ids(all), err)
	}
}

// GetBy returns the row, or sql.ErrNoRows when none matches; the named
// tenant type is converted.
func TestDerivedGetReturnsErrNoRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item, err := f.repo.GetByTenantIDAndID(ctx, TenantID(f.tenant), "a1")
	if err != nil || item.ID != "a1" || item.TenantID != f.tenant {
		t.Fatalf("GetByTenantIDAndID: got %+v, %v", item, err)
	}
	if _, err = f.repo.GetByTenantIDAndID(ctx, TenantID(f.other), "a1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another tenant's row: err = %v, want sql.ErrNoRows", err)
	}
}

// Derived exists and counts agree with the hand-written ones.
func TestDerivedExistsAndCountMatchHandWritten(t *testing.T) {
	f := newFixture(t)
	ctx, r, w := context.Background(), f.repo, f.repo.where
	for _, tenant := range []uuid.UUID{f.tenant, f.other} {
		derived, err := r.ExistsByTenantIDAndExternalID(ctx, tenant, "ext-1")
		hand, handErr := r.exists(ctx, w.TenantID(tenant).AND(w.ExternalID("ext-1")))
		if err != nil || handErr != nil || derived != hand || derived != (tenant == f.tenant) {
			t.Errorf("ExistsByTenantIDAndExternalID: got %v, %v; hand-written %v, %v", derived, err, hand, handErr)
		}
		for _, active := range []bool{true, false} {
			n, countErr := r.CountByTenantIDAndActive(ctx, tenant, active)
			want, wantErr := r.count(ctx, w.TenantID(tenant).AND(w.Active(active)))
			if countErr != nil || wantErr != nil || n != want {
				t.Errorf("CountByTenantIDAndActive(%v): got %d, %v; hand-written %d, %v", active, n, countErr, want,
					wantErr)
			}
		}
	}
	if n, err := r.CountByTenantIDAndActive(ctx, f.tenant, true); err != nil || n != 2 {
		t.Fatalf("CountByTenantIDAndActive: got %d, %v; want 2", n, err)
	}
	// The hand-written ExistsByTenantIDAndName wins over the derived name:
	// it ignores case.
	if found, err := r.ExistsByTenantIDAndName(ctx, f.tenant, "BLUE"); err != nil || !found {
		t.Fatalf("ExistsByTenantIDAndName: got %v, %v; want the case-insensitive match", found, err)
	}
}

// Derived deletes remove the rows of the tenant only, in their three result
// shapes.
func TestDerivedDeletes(t *testing.T) {
	f := newFixture(t)
	ctx, r := context.Background(), f.repo
	if err := r.DeleteByTenantIDAndID(ctx, f.other, "a1"); err != nil {
		t.Fatal(err)
	}
	if found, _ := r.FindByTenantIDAndID(ctx, f.tenant, "a1"); !found.IsPresent() {
		t.Fatal("a delete with another tenant removed a1")
	}
	if err := r.DeleteByTenantIDAndID(ctx, f.tenant, "a1"); err != nil {
		t.Fatal(err)
	}
	if found, _ := r.FindByTenantIDAndID(ctx, f.tenant, "a1"); found.IsPresent() {
		t.Fatal("a1 is still there")
	}
	if deleted, err := r.DeleteByTenantIDAndSku(ctx, f.tenant, "A-2"); err != nil || !deleted {
		t.Fatalf("DeleteByTenantIDAndSku: got %v, %v; want true", deleted, err)
	}
	if deleted, err := r.DeleteByTenantIDAndSku(ctx, f.tenant, "A-2"); err != nil || deleted {
		t.Fatalf("DeleteByTenantIDAndSku again: got %v, %v; want false", deleted, err)
	}
	if n, err := r.DeleteByTenantIDAndStatus(ctx, f.tenant, model.ItemStatus_Archived); err != nil || n != 1 {
		t.Fatalf("DeleteByTenantIDAndStatus: got %d, %v; want 1", n, err)
	}
	if n, err := r.CountInternal(ctx); err != nil || n != 1 {
		t.Fatalf("CountInternal: got %d, %v; want only b1 left", n, err)
	}
}

// A single-row find whose key a migration dropped returns
// transactor.ErrMoreThanOneRow on a second row, never a silent first row.
func TestStaleUniqueKeyIsAnError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.db.ExecContext(ctx, `ALTER TABLE items DROP CONSTRAINT items_tenant_sku_key`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Create(ctx, model.Items{ID: "a4", TenantID: f.tenant, Sku: "A-1", Name: "twin",
		Status: model.ItemStatus_Active, CreatedAt: f.created}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.FindByTenantIDAndSku(ctx, f.tenant, "A-1"); !errors.Is(err, transactor.ErrMoreThanOneRow) {
		t.Fatalf("err = %v, want transactor.ErrMoreThanOneRow", err)
	}
}

// Derived deletes refuse a context row lock, as deleteWhere does; derived
// finds take it.
func TestDerivedRowLocks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.repo.DeleteByTenantIDAndID(transactor.ForUpdate(ctx), f.tenant, "a1"); !errors.Is(err,
		transactor.ErrRowLockOutsideTx) {
		t.Fatalf("locked delete: err = %v, want ErrRowLockOutsideTx", err)
	}
	err := transactor.New(f.db).InTx(ctx, func(txCtx context.Context) error {
		if _, err := f.repo.FindByTenantIDAndID(transactor.ForUpdate(txCtx), f.tenant, "a1"); err != nil {
			return err
		}
		if lockable(t, f.db, "a1") {
			t.Error("FindByTenantIDAndID under ForUpdate did not lock a1")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func sameOption(t *testing.T, name string, got functional.Option[Item], err error, want functional.Option[Item],
	wantErr error,
) {
	t.Helper()
	if err != nil || wantErr != nil || got != want {
		t.Errorf("%s: got %+v, %v; hand-written %+v, %v", name, got, err, want, wantErr)
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}
