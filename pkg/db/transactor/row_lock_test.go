package transactor_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

var (
	lockedID    = postgres.StringColumn("id")
	lockedTable = postgres.NewTable("public", "it_locked", "", lockedID)
)

func selectLockedRow() postgres.SelectStatement {
	return lockedTable.SELECT(lockedID).WHERE(lockedID.EQ(postgres.String("a")))
}

func inFakeTx(ctx context.Context) context.Context {
	return transactor.WithTx(ctx, &sql.Tx{})
}

func TestRowLockAddsTheRequestedClause(t *testing.T) {
	for name, test := range map[string]struct {
		lock func(context.Context) context.Context
		want string
	}{
		"for share":         {transactor.ForShare, "FOR SHARE"},
		"for update":        {transactor.ForUpdate, "FOR UPDATE"},
		"for no key update": {transactor.ForNoKeyUpdate, "FOR NO KEY UPDATE"},
		"for key share":     {transactor.ForKeyShare, "FOR KEY SHARE"},
		"nowait": {func(ctx context.Context) context.Context {
			return transactor.WithRowLock(ctx, postgres.UPDATE().NOWAIT())
		}, "FOR UPDATE NOWAIT"},
	} {
		t.Run(name, func(t *testing.T) {
			lockedCtx := test.lock(inFakeTx(context.Background()))
			stmt, err := transactor.WithRequestedRowLockForTest(lockedCtx, selectLockedRow())
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if query, _ := stmt.Sql(); !strings.HasSuffix(strings.TrimSpace(query), test.want+";") {
				t.Fatalf("query %q does not end with %s", query, test.want)
			}
		})
	}
}

func TestRowLockLeavesAnUnlockedQueryAlone(t *testing.T) {
	stmt := selectLockedRow()
	got, err := transactor.WithRequestedRowLockForTest(context.Background(), stmt)
	if err != nil || got != stmt {
		t.Fatalf("got %v, %v; want the statement untouched", got, err)
	}
}

func TestRowLockRefusesAQueryOutsideATransaction(t *testing.T) {
	_, err := transactor.WithRequestedRowLockForTest(transactor.ForShare(context.Background()), selectLockedRow())
	if !errors.Is(err, transactor.ErrRowLockOutsideTx) {
		t.Fatalf("err = %v, want ErrRowLockOutsideTx", err)
	}
}

func TestRowLockRefusesAStatementThatIsNotASelect(t *testing.T) {
	stmt := lockedTable.DELETE().WHERE(lockedID.EQ(postgres.String("a")))
	_, err := transactor.WithRequestedRowLockForTest(transactor.ForUpdate(inFakeTx(context.Background())), stmt)
	if !errors.Is(err, transactor.ErrRowLockNotSelect) {
		t.Fatalf("err = %v, want ErrRowLockNotSelect", err)
	}
}

// TestRowLockWritesExactlyOneForClause pins the whole SQL text of each lock
// helper. Nanostack DB's FOR is variadic and replaces the statement's lock
// list (go-jet afea45b), so a locked context must still write one FOR clause,
// and a lock the statement already had is replaced, never kept next to it.
func TestRowLockWritesExactlyOneForClause(t *testing.T) {
	const selectSQL = "\nSELECT it_locked.id AS \"it_locked.id\"\n" +
		"FROM public.it_locked\n" +
		"WHERE it_locked.id = $1::text\n"
	for name, test := range map[string]struct {
		lock func(context.Context) context.Context
		want string
	}{
		"for update":        {transactor.ForUpdate, selectSQL + "FOR UPDATE;\n"},
		"for share":         {transactor.ForShare, selectSQL + "FOR SHARE;\n"},
		"for no key update": {transactor.ForNoKeyUpdate, selectSQL + "FOR NO KEY UPDATE;\n"},
		"for key share":     {transactor.ForKeyShare, selectSQL + "FOR KEY SHARE;\n"},
	} {
		t.Run(name, func(t *testing.T) {
			for baseName, base := range map[string]postgres.SelectStatement{
				"unlocked statement":      selectLockedRow(),
				"statement with own lock": selectLockedRow().FOR(postgres.SHARE().NOWAIT()),
			} {
				t.Run(baseName, func(t *testing.T) {
					stmt, err := transactor.WithRequestedRowLockForTest(test.lock(inFakeTx(context.Background())), base)
					if err != nil {
						t.Fatalf("err = %v", err)
					}
					query, args := stmt.Sql()
					if query != test.want {
						t.Errorf("query = %q, want %q", query, test.want)
					}
					if got := strings.Count(query, "\nFOR "); got != 1 {
						t.Errorf("query has %d FOR clauses, want 1: %q", got, query)
					}
					if len(args) != 1 || args[0] != "a" {
						t.Errorf("args = %v, want [a]", args)
					}
				})
			}
		})
	}
}
