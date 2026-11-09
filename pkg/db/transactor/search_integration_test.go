//go:build integration

package transactor_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/pgerr"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// The search forwarders against PostgreSQL, on the it_keyset table of the
// keyset tests: each of the four runs in the context transaction of InTx,
// refuses a row lock there, and composes with the On* translations.

// ksSearch is the builder of a search of it_keyset scoped on its tenant, as
// nano gen repo writes one.
type ksSearch struct {
	searchexec.Query[ksRow, ksSearch, ksField, string]
}

// ksSearchSpec is the tenant search of from, sorted by name by default.
func ksSearchSpec(from jet.ReadableTable) *searchexec.Spec[ksRow, ksField, string] {
	return searchexec.DefineSpec(searchexec.SpecDef[ksRow, ksField, string]{
		From:    func(string) keyset.Source { return from },
		Columns: jet.ProjectionList(ksAll),
		Scope:   []searchexec.ScopeCol[string]{searchexec.ScopeEq(searchexec.Value[string], ksTenantC)},
		//nolint:exhaustive // the search sorts only by name; a test adds other keys with plan.Order
		Keys: map[ksField]keyset.Key[ksRow]{
			ksName: keyset.StringKey(ksNameC, func(r ksRow) string { return r.Name }),
		},
		Tiebreak: []keyset.Key[ksRow]{keyset.StringKey(ksID, func(r ksRow) string { return r.ID })},
		Default:  searchexec.Asc(ksName),
		Limit:    10,
	})
}

func ksRowID(r ksRow) string { return r.ID }

func ksRowIDs(_ context.Context, rows []ksRow) ([]string, error) {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids, nil
}

// ksRun runs one of the four forwarders. Each run returns the Result
// unchanged, so a test can chain On* on it.
type ksRun func(
	ctx context.Context, db *sql.DB, spec *searchexec.Spec[ksRow, ksField, string], tenant string,
) transactor.Result[dbsearch.Result[string]]

// ksRuns are the four forwarders. RunPlan and RunPlanPage take the shape of a
// hand-written method: Resolve, And, Order, then the transactor.
func ksRuns() map[string]ksRun {
	query := func(spec *searchexec.Spec[ksRow, ksField, string], tenant string) searchexec.Query[ksRow, ksSearch, ksField, string] {
		return searchexec.New[ksSearch](spec, tenant).Query
	}
	plan := func(
		ctx context.Context, spec *searchexec.Spec[ksRow, ksField, string], tenant string,
	) (searchexec.Plan[ksRow, ksField], keyset.Order[ksRow], error) {
		p, err := searchexec.Resolve(ctx, spec, query(spec, tenant))
		if err != nil {
			return p, keyset.Order[ksRow]{}, err
		}
		p = p.And(ksRankC.GT_EQ(jet.Int(0)))
		order, err := p.Order(nil)
		return p, order, err
	}
	return map[string]ksRun{
		"Search": func(ctx context.Context, db *sql.DB, spec *searchexec.Spec[ksRow, ksField, string], tenant string) transactor.Result[dbsearch.Result[string]] {
			return transactor.Search(ctx, db, spec, query(spec, tenant), ksRowID)
		},
		"SearchPage": func(ctx context.Context, db *sql.DB, spec *searchexec.Spec[ksRow, ksField, string], tenant string) transactor.Result[dbsearch.Result[string]] {
			return transactor.SearchPage(ctx, db, spec, query(spec, tenant), ksRowIDs)
		},
		"RunPlan": func(ctx context.Context, db *sql.DB, spec *searchexec.Spec[ksRow, ksField, string], tenant string) transactor.Result[dbsearch.Result[string]] {
			p, order, err := plan(context.Background(), spec, tenant)
			if err != nil {
				return transactor.NewResultForTest(dbsearch.Result[string]{}, err)
			}
			return transactor.RunPlan(ctx, db, p, order, ksRowID)
		},
		"RunPlanPage": func(ctx context.Context, db *sql.DB, spec *searchexec.Spec[ksRow, ksField, string], tenant string) transactor.Result[dbsearch.Result[string]] {
			p, order, err := plan(context.Background(), spec, tenant)
			if err != nil {
				return transactor.NewResultForTest(dbsearch.Result[string]{}, err)
			}
			return transactor.RunPlanPage(ctx, db, p, order, ksRowIDs)
		},
	}
}

func ksInsertNamed(t *testing.T, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, id, tenant, name string,
) {
	t.Helper()
	if _, err := exec.ExecContext(context.Background(), `INSERT INTO it_keyset
		(id, tenant, name, created, status, rank, ref)
		VALUES ($1, $2, $3, now(), 'active', 1, gen_random_uuid())`, id, tenant, name); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

// undefinedTable is the SQLSTATE of a table that does not exist.
const undefinedTable = "42P01"

var errSearchRollback = errors.New("roll back the search test transaction")

// Each forwarder runs in the transaction of InTx: it sees the row that the
// transaction wrote and did not commit, and only the rows of its tenant.
// After the rollback the row is gone.
func TestIntegrationSearchRunsInTheContextTransaction(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	ksInsertNamed(t, db, "committed", "t_search", "b")
	ksInsertNamed(t, db, "other_tenant", "t_other", "a")
	spec := ksSearchSpec(ksTable)
	tx := transactor.New(db)

	for name, run := range ksRuns() {
		t.Run(name, func(t *testing.T) {
			err := tx.InTx(t.Context(), func(txCtx context.Context) error {
				ksInsertNamed(t, transactor.CurrentTx(txCtx), "tx_only", "t_search", "a")
				result, err := run(txCtx, db, spec, "t_search").Value()
				if err != nil {
					t.Fatalf("%s in the transaction: %v", name, err)
				}
				if want := []string{"tx_only", "committed"}; result.Total != 2 || !slices.Equal(result.Items, want) {
					t.Fatalf("%s in the transaction = %+v, want Total 2 and %v", name, result, want)
				}
				return errSearchRollback
			})
			if !errors.Is(err, errSearchRollback) {
				t.Fatalf("InTx = %v, want the rollback sentinel", err)
			}
			result, err := run(t.Context(), db, spec, "t_search").Value()
			if err != nil {
				t.Fatalf("%s after the rollback: %v", name, err)
			}
			if result.Total != 1 || !slices.Equal(result.Items, []string{"committed"}) {
				t.Fatalf("%s after the rollback = %+v, want only the committed row", name, result)
			}
		})
	}
}

// In the transaction of InTx, each forwarder refuses a requested row lock with
// ErrRowLockSearch and runs nothing, so the transaction stays usable.
func TestIntegrationSearchRefusesARowLockInTx(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	ksInsertNamed(t, db, "committed", "t_search", "b")
	spec := ksSearchSpec(ksTable)
	tx := transactor.New(db)

	for name, run := range ksRuns() {
		t.Run(name, func(t *testing.T) {
			err := tx.InTx(t.Context(), func(txCtx context.Context) error {
				for lockName, lock := range map[string]func(context.Context) context.Context{
					"ForShare":  transactor.ForShare,
					"ForUpdate": transactor.ForUpdate,
				} {
					if _, err := run(
						lock(txCtx),
						db,
						spec,
						"t_search",
					).Value(); !errors.Is(
						err,
						transactor.ErrRowLockSearch,
					) {
						t.Fatalf("%s under %s = %v, want ErrRowLockSearch", name, lockName, err)
					}
				}
				// The refusal ran no statement: the transaction is not aborted.
				result, err := run(txCtx, db, spec, "t_search").Value()
				if err != nil || result.Total != 1 {
					t.Fatalf("%s after the refusals = %+v, %v; want the committed row", name, result, err)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("InTx: %v", err)
			}
		})
	}
}

// The forwarders return a Result, so the On* translations compose before
// Value: the driver's error reaches them through searchexec and db/query.
func TestIntegrationSearchValueComposes(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	spec := ksSearchSpec(ksTable)
	tx := transactor.New(db)
	errTarget := errors.New("the search table is missing")

	for name, run := range ksRuns() {
		t.Run(name, func(t *testing.T) {
			for _, translate := range []bool{false, true} {
				// The table is dropped in a transaction that rolls back, so the
				// shared it_keyset columns keep their table.
				err := tx.InTx(t.Context(), func(txCtx context.Context) error {
					if _, err := transactor.CurrentTx(txCtx).ExecContext(txCtx, `DROP TABLE it_keyset`); err != nil {
						t.Fatalf("drop: %v", err)
					}
					result := run(txCtx, db, spec, "t_search")
					if translate {
						if _, err := result.OnSQLState(undefinedTable, errTarget).Value(); !errors.Is(err, errTarget) {
							t.Fatalf("%s.OnSQLState(...).Value() = %v, want the translated error", name, err)
						}
					} else if _, err := result.Value(); !pgerr.Is(err, undefinedTable) {
						t.Fatalf("%s on a missing table = %v, want SQLSTATE %s", name, err, undefinedTable)
					}
					return errSearchRollback
				})
				if !errors.Is(err, errSearchRollback) {
					t.Fatalf("InTx = %v, want the rollback sentinel", err)
				}
			}
		})
	}
}

// Search and SearchPage refuse a query built from another spec against the
// database too, and run nothing: the row of the other spec's tenant is not
// read.
func TestIntegrationSearchRefusesAForeignSpec(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	ksInsertNamed(t, db, "committed", "t_search", "b")
	spec, other := ksSearchSpec(ksTable), ksSearchSpec(ksTable)
	foreign := searchexec.New[ksSearch](other, "t_search").Query

	if _, err := transactor.Search(t.Context(), db, spec, foreign, ksRowID).
		Value(); !errors.Is(
		err,
		dbsearch.ErrUnscoped,
	) {
		t.Fatalf("Search with a foreign spec = %v, want search.ErrUnscoped", err)
	}
	if _, err := transactor.SearchPage(t.Context(), db, spec, foreign, ksRowIDs).
		Value(); !errors.Is(
		err,
		dbsearch.ErrUnscoped,
	) {
		t.Fatalf("SearchPage with a foreign spec = %v, want search.ErrUnscoped", err)
	}
}

// ksHandRank is a sort that the spec does not declare: a hand-written method
// adds its key with plan.Order(extra), as EP2 does for request_count.
const ksHandRank ksField = "HAND_RANK"

// RunPlan and RunPlanPage read in the order the caller passes, so a sort on a
// hand-written key works.
func TestIntegrationRunPlanUsesTheCallersOrder(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	for _, r := range []struct {
		id, name string
		rank     int
	}{{"low", "a", 1}, {"high", "b", 3}, {"mid", "c", 2}} {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO it_keyset
			(id, tenant, name, created, status, rank, ref)
			VALUES ($1, 't_search', $2, now(), 'active', $3, gen_random_uuid())`, r.id, r.name, r.rank); err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}
	spec := ksSearchSpec(ksTable)
	q := searchexec.New[ksSearch](spec, "t_search").SortBy(ksHandRank, dbsearch.SortDescending)
	plan, err := searchexec.Resolve(t.Context(), spec, q.Query)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	//nolint:exhaustive // the hand-written method adds only its own key
	order, err := plan.Order(map[ksField]keyset.Key[ksRow]{
		ksHandRank: keyset.IntKey(ksRankC, func(r ksRow) int64 { return r.Rank }),
	})
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	want := []string{"high", "mid", "low"}
	for name, run := range map[string]func(context.Context) (dbsearch.Result[string], error){
		"RunPlan": func(ctx context.Context) (dbsearch.Result[string], error) {
			return transactor.RunPlan(ctx, db, plan, order, ksRowID).Value()
		},
		"RunPlanPage": func(ctx context.Context) (dbsearch.Result[string], error) {
			return transactor.RunPlanPage(ctx, db, plan, order, ksRowIDs).Value()
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, runErr := run(t.Context())
			if runErr != nil || !slices.Equal(result.Items, want) {
				t.Fatalf("%s by the hand-written key = %+v, %v; want %v", name, result, runErr, want)
			}
		})
	}
}
