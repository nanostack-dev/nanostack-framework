//go:build integration

package torture

import (
	"context"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/pgerr"
)

// A foreign key proves product_id -> org_id only where it checks every row:
// PostgreSQL's default MATCH SIMPLE skips it when a referencing column is
// NULL, and a NOT VALID key leaves the rows that predate it unchecked. The
// generator takes both from the catalogue and refuses the implication (the
// golden of testdata/torture/refused); these tests show on PostgreSQL why.
func TestForeignKeyProofNeedsEnforcement(t *testing.T) {
	db := openTorture(t)
	mustExec(t, db, `INSERT INTO tproduct (id, org_id) VALUES ($1,$2), ($3,$4)`,
		products[0], orgs[0], products[1], orgs[1])

	// MATCH SIMPLE, nullable org_id: a row (product, NULL) passes the key. A derived
	// FindByProductAndName on it would return that row to every org.
	mustExec(t, db, `INSERT INTO bin_null (id, org_id, product_id, name) VALUES ('orphan', NULL, $1, 'n')`, products[0])
	// MATCH FULL rejects the same row.
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO bin_full (id, org_id, product_id, name) VALUES ('orphan', NULL, $1, 'n')`, products[0])
	if !pgerr.IsForeignKeyViolation(err, "bin_full_product_id_org_id_fkey") {
		t.Errorf("MATCH FULL row (product, NULL): %v, want a foreign key violation", err)
	}
	// A product under the wrong org is refused by both.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO bin_full (id, org_id, product_id, name) VALUES ('wrong', $1, $2, 'n')`, orgs[1], products[0])
	if !pgerr.IsForeignKeyViolation(err, "bin_full_product_id_org_id_fkey") {
		t.Errorf("MATCH FULL row under the wrong org: %v, want a foreign key violation", err)
	}

	// NOT VALID: the legacy row of the migration breaks the key, and the table still accepts
	// it, while a new bad row is refused.
	var broken int
	row := db.QueryRowContext(context.Background(), `SELECT count(*) FROM bin_nv b WHERE NOT EXISTS
		(SELECT 1 FROM tproduct p WHERE p.id = b.product_id AND p.org_id = b.org_id)`)
	if err = row.Scan(&broken); err != nil || broken != 1 {
		t.Errorf("rows of bin_nv that break the NOT VALID key: %d %v, want the 1 legacy row", broken, err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO bin_nv (id, org_id, product_id, name) VALUES ('new', $1, $2, 'n')`, orgs[1], products[0])
	if !pgerr.IsForeignKeyViolation(err, "bin_nv_product_fk") {
		t.Errorf("new row on the NOT VALID key: %v, want a foreign key violation", err)
	}

	// MATCH FULL proves the implication again: Product alone names both columns, and reads and
	// deletes see only that product's rows.
	mustExec(
		t,
		db,
		`INSERT INTO bin_full (id, org_id, product_id, name) VALUES ('p0', $1, $2, 'n'), ('p1', $3, $4, 'n')`,
		orgs[0],
		products[0],
		orgs[1],
		products[1],
	)
	bins := NewBinFulls(db)
	ctx := context.Background()
	got, err := bins.FindByProductAndName(ctx, products[0], "n")
	if err != nil || len(got) != 1 || got[0].ID != "p0" {
		t.Errorf("FindByProductAndName: %v %v, want p0 only", got, err)
	}
	if n, dErr := bins.DeleteByProductAndName(ctx, products[1], "n"); dErr != nil || n != 1 {
		t.Errorf("DeleteByProductAndName: %d %v, want 1", n, dErr)
	}
	if left := column(t, db, `SELECT id FROM bin_full`); len(left) != 1 || left[0] != "p0" {
		t.Errorf("bin_full holds %v, want p0", left)
	}
}
