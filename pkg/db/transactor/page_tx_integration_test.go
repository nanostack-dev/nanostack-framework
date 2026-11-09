//go:build integration

package transactor_test

import (
	"context"
	"testing"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// A page runs in the context transaction: it sees a row that the transaction
// wrote and did not commit.
func TestIntegrationPageRunsInTheContextTransaction(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO it_keyset (id, tenant, name, created, status, rank, ref)
		VALUES ('tx_only', 't_tx', 'n', now(), 'active', 1, gen_random_uuid())`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	result, err := transactor.Page(db, func(r ksRow) ksRow { return r }, ksAll...).
		From(ksTable).
		Where(ksTenantC.EQ(jet.String("t_tx"))).
		Keyset(transactor.Keys[ksField](nil, ksKeys(), ksTiebreak())).
		Run(transactor.WithTx(ctx, tx), search.Pagination{Limit: 10}).
		Value()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != "tx_only" {
		t.Fatalf("Run in the transaction = %+v, want the uncommitted row", result)
	}
}
