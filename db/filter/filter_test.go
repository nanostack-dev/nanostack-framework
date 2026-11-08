package filter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/filter"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

var (
	colA  = IntegerColumn("a")
	colB  = StringColumn("b")
	table = NewTable("db", "t", "", colA, colB)

	condA = colA.EQ(Int(1))
	condB = colB.EQ(String("x"))
)

func whereSQL(t *testing.T, cond filter.BoolExpression) string {
	t.Helper()
	sql, _ := SELECT(colA).FROM(table).WHERE(cond).Sql()
	return sql
}

func TestWhen(t *testing.T) {
	require.Nil(
		t,
		filter.When(functional.None[int64](), func(v int64) filter.BoolExpression { return colA.EQ(Int(v)) }),
	)

	got := filter.When(functional.Some[int64](3), func(v int64) filter.BoolExpression { return colA.EQ(Int(v)) })
	require.Equal(t, whereSQL(t, colA.EQ(Int(3))), whereSQL(t, got))
}

func TestIf(t *testing.T) {
	require.Nil(t, filter.If(false, func() filter.BoolExpression {
		t.Fatal("cond must not be called when ok is false")
		return nil
	}))
	require.Equal(t, condA, filter.If(true, func() filter.BoolExpression { return condA }))
}

func TestAll(t *testing.T) {
	require.Nil(t, filter.All())
	require.Nil(t, filter.All(nil, nil))
	require.Equal(t, condA, filter.All(condA), "one condition is returned unchanged")
	require.Equal(t, condA, filter.All(nil, condA, nil))
	require.Equal(t, whereSQL(t, AND(condA, condB)), whereSQL(t, filter.All(condA, nil, condB)))
}

func TestAny(t *testing.T) {
	require.Nil(t, filter.Any())
	require.Equal(t, condB, filter.Any(nil, condB))
	require.Equal(t, whereSQL(t, OR(condA, condB)), whereSQL(t, filter.Any(condA, nil, condB)))
}

func TestNoConditionMeansNoWhere(t *testing.T) {
	require.Equal(t, `
SELECT t.a AS "t.a"
FROM db.t;
`, whereSQL(t, filter.All()))
}

func TestWritesKeepTheirMandatoryWhere(t *testing.T) {
	require.PanicsWithValue(t, "jet: WHERE clause not set", func() {
		_, _ = table.UPDATE(colB).SET(String("y")).WHERE(filter.All()).Sql()
	})
	require.PanicsWithValue(t, "jet: WHERE clause not set", func() {
		_, _ = table.DELETE().WHERE(filter.All()).Sql()
	})
}
