package transactor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// A small search, written by hand as nanostack gen repo writes one. The unit tests
// below never reach a database: every case fails before a statement runs.

type srRow struct {
	ID     string
	Tenant string
}

type srSort string

const srID srSort = "id"

type srSearch struct {
	searchexec.Query[srRow, srSearch, srSort, string]
}

var (
	srIDCol     = jet.StringColumn("id")
	srTenantCol = jet.StringColumn("tenant")
	srTable     = jet.NewTable("public", "search_rows", "", srIDCol, srTenantCol)
)

func srSpec() *searchexec.Spec[srRow, srSort, string] {
	idKey := keyset.StringKey(srIDCol, func(r srRow) string { return r.ID })
	return searchexec.DefineSpec(searchexec.SpecDef[srRow, srSort, string]{
		From:     func(string) keyset.Source { return srTable },
		Columns:  jet.ProjectionList{srIDCol, srTenantCol},
		Scope:    []searchexec.ScopeCol[string]{searchexec.ScopeEq(searchexec.Value[string], srTenantCol)},
		Keys:     map[srSort]keyset.Key[srRow]{srID: idKey},
		Tiebreak: []keyset.Key[srRow]{idKey},
		Limit:    10,
	})
}

func srIdentity(r srRow) srRow { return r }

func srIdentityPage(_ context.Context, rows []srRow) ([]srRow, error) { return rows, nil }

// srRuns are the four transactor entry points, each run once with the same
// search, so a test states its rule for all four.
func srRuns(
	spec *searchexec.Spec[srRow, srSort, string], q srSearch,
) map[string]func(context.Context, *countingDB) error {
	return map[string]func(context.Context, *countingDB) error{
		"Search": func(ctx context.Context, db *countingDB) error {
			return transactor.Search(ctx, db, spec, q.Query, srIdentity).Err()
		},
		"SearchPage": func(ctx context.Context, db *countingDB) error {
			return transactor.SearchPage(ctx, db, spec, q.Query, srIdentityPage).Err()
		},
		"RunPlan": func(ctx context.Context, db *countingDB) error {
			plan, order, err := srPlan(spec, q)
			if err != nil {
				return err
			}
			return transactor.RunPlan(ctx, db, plan, order, srIdentity).Err()
		},
		"RunPlanPage": func(ctx context.Context, db *countingDB) error {
			plan, order, err := srPlan(spec, q)
			if err != nil {
				return err
			}
			return transactor.RunPlanPage(ctx, db, plan, order, srIdentityPage).Err()
		},
	}
}

// srPlan resolves q as a hand-written method does. It resolves outside the
// row-locked context, so the test reaches the transactor's own check.
func srPlan(
	spec *searchexec.Spec[srRow, srSort, string], q srSearch,
) (searchexec.Plan[srRow, srSort], keyset.Order[srRow], error) {
	plan, err := searchexec.Resolve(context.Background(), spec, q.Query)
	if err != nil {
		return plan, keyset.Order[srRow]{}, err
	}
	order, err := plan.Order(nil)
	return plan, order, err
}

// A search page never locks rows: under every requested row lock, inside or
// outside a transaction, each of the four returns ErrRowLockSearch and runs
// no statement.
func TestSearchRefusesARowLock(t *testing.T) {
	spec := srSpec()
	q := searchexec.New[srSearch](spec, "t1")
	locks := map[string]func(context.Context) context.Context{
		"ForShare":       transactor.ForShare,
		"ForUpdate":      transactor.ForUpdate,
		"ForNoKeyUpdate": transactor.ForNoKeyUpdate,
		"ForKeyShare":    transactor.ForKeyShare,
		"WithRowLock": func(ctx context.Context) context.Context {
			return transactor.WithRowLock(ctx, jet.UPDATE().SKIP_LOCKED())
		},
	}
	for run, fn := range srRuns(spec, q) {
		for lockName, lock := range locks {
			t.Run(run+"/"+lockName, func(t *testing.T) {
				// The fake transaction cannot run a statement, and countingDB
				// records one: the error must come before any query.
				for _, ctx := range []context.Context{t.Context(), inFakeTx(t.Context())} {
					db := &countingDB{}
					err := fn(lock(ctx), db)
					if !errors.Is(err, transactor.ErrRowLockSearch) {
						t.Fatalf("%s under %s = %v, want ErrRowLockSearch", run, lockName, err)
					}
					if db.ran {
						t.Fatalf("%s under %s ran a statement", run, lockName)
					}
				}
			})
		}
	}
}

// Search and SearchPage take the search's spec and refuse a query built from
// another spec, a nil spec and a struct literal of the builder, before any
// statement runs.
func TestSearchRefusesAForeignSpec(t *testing.T) {
	spec, other := srSpec(), srSpec()
	for name, test := range map[string]struct {
		spec *searchexec.Spec[srRow, srSort, string]
		q    srSearch
	}{
		"another spec":   {spec, searchexec.New[srSearch](other, "t1")},
		"nil spec":       {nil, searchexec.New[srSearch](spec, "t1")},
		"struct literal": {spec, srSearch{}},
	} {
		for _, run := range []string{"Search", "SearchPage"} {
			t.Run(name+"/"+run, func(t *testing.T) {
				db := &countingDB{}
				err := srRuns(test.spec, test.q)[run](t.Context(), db)
				if !errors.Is(err, dbsearch.ErrUnscoped) {
					t.Fatalf("%s with %s = %v, want search.ErrUnscoped", run, name, err)
				}
				if db.ran {
					t.Fatalf("%s with %s ran a statement", run, name)
				}
			})
		}
	}
}

// RunPlan and RunPlanPage run only a plan that searchexec.Resolve returned:
// the zero Plan is search.ErrUnscoped, and no statement runs.
func TestRunPlanRefusesTheZeroPlan(t *testing.T) {
	order, orderErr := srPlanOrder()
	if orderErr != nil {
		t.Fatalf("Order: %v", orderErr)
	}
	var plan searchexec.Plan[srRow, srSort]
	for name, run := range map[string]func(*countingDB) error{
		"RunPlan": func(db *countingDB) error {
			return transactor.RunPlan(t.Context(), db, plan, order, srIdentity).Err()
		},
		"RunPlanPage": func(db *countingDB) error {
			return transactor.RunPlanPage(t.Context(), db, plan, order, srIdentityPage).Err()
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := &countingDB{}
			if err := run(db); !errors.Is(err, dbsearch.ErrUnscoped) {
				t.Fatalf("%s with the zero Plan = %v, want search.ErrUnscoped", name, err)
			}
			if db.ran {
				t.Fatalf("%s with the zero Plan ran a statement", name)
			}
		})
	}
}

// srPlanOrder is a valid order, so the zero-plan test fails on the plan alone.
func srPlanOrder() (keyset.Order[srRow], error) {
	spec := srSpec()
	_, order, err := srPlan(spec, searchexec.New[srSearch](spec, "t1"))
	return order, err
}

// A check that searchexec makes reaches the caller unchanged: here a page that
// search.CheckPage refuses.
func TestSearchReturnsTheChecksOfSearchexec(t *testing.T) {
	spec := srSpec()
	q := searchexec.New[srSearch](spec, "t1").Limit(0)
	for _, run := range []string{"Search", "SearchPage"} {
		t.Run(run, func(t *testing.T) {
			db := &countingDB{}
			err := srRuns(spec, q)[run](t.Context(), db)
			if !errors.Is(err, dbsearch.ErrInvalidLimit) {
				t.Fatalf("%s with Limit(0) = %v, want search.ErrInvalidLimit", run, err)
			}
			if db.ran {
				t.Fatalf("%s with Limit(0) ran a statement", run)
			}
		})
	}
}

// RunPlan and RunPlanPage refuse the zero Order: it has no key, so the page
// would read in no order and give no cursor.
func TestRunPlanRefusesTheZeroOrder(t *testing.T) {
	spec := srSpec()
	plan, _, err := srPlan(spec, searchexec.New[srSearch](spec, "t1"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for name, run := range map[string]func(*countingDB) error{
		"RunPlan": func(db *countingDB) error {
			return transactor.RunPlan(t.Context(), db, plan, keyset.Order[srRow]{}, srIdentity).Err()
		},
		"RunPlanPage": func(db *countingDB) error {
			return transactor.RunPlanPage(t.Context(), db, plan, keyset.Order[srRow]{}, srIdentityPage).Err()
		},
	} {
		db := &countingDB{}
		if runErr := run(db); runErr == nil || db.ran {
			t.Fatalf("%s with the zero Order = %v (ran %v), want an error before any statement", name, runErr, db.ran)
		}
	}
}
