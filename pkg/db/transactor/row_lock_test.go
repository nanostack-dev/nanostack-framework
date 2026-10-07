package transactor_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/go-jet/jet/v2/postgres"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

var (
	lockedID    = postgres.StringColumn("id")
	lockedTable = postgres.NewTable("public", "it_locked", "", lockedID)
)

func selectLockedRow(id string) postgres.SelectStatement {
	return lockedTable.SELECT(lockedID).WHERE(lockedID.EQ(postgres.String(id)))
}

func inFakeTx(ctx context.Context) context.Context {
	return transactor.WithTx(ctx, &sql.Tx{})
}

func TestRowLockAddsTheRequestedClause(t *testing.T) {
	for name, test := range map[string]struct {
		lock func(context.Context) context.Context
		want string
	}{
		"for share":  {transactor.ForShare, "FOR SHARE"},
		"for update": {transactor.ForUpdate, "FOR UPDATE"},
		"nowait": {func(ctx context.Context) context.Context {
			return transactor.WithRowLock(ctx, postgres.UPDATE().NOWAIT())
		}, "FOR UPDATE NOWAIT"},
	} {
		t.Run(name, func(t *testing.T) {
			lockedCtx := test.lock(inFakeTx(context.Background()))
			stmt, err := transactor.WithRequestedRowLockForTest(lockedCtx, selectLockedRow("a"))
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
	stmt := selectLockedRow("a")
	got, err := transactor.WithRequestedRowLockForTest(context.Background(), stmt)
	if err != nil || got != stmt {
		t.Fatalf("got %v, %v; want the statement untouched", got, err)
	}
}

func TestRowLockRefusesAQueryOutsideATransaction(t *testing.T) {
	_, err := transactor.WithRequestedRowLockForTest(transactor.ForShare(context.Background()), selectLockedRow("a"))
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
