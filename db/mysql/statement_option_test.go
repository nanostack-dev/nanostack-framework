// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package mysql

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/internal/testutils"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

type optTable3 struct {
	Col1   int64
	ColInt functional.Option[int64]
	Col2   functional.Option[string]
}

func TestInsertModelOption(t *testing.T) {
	stmt := table3.INSERT(table3Col1, table3ColInt, table3StrCol).
		MODEL(optTable3{Col1: 1, ColInt: functional.Some[int64](0)})

	assertStatementSql(t, stmt, `
INSERT INTO db.table3 (col1, col_int, col2)
VALUES (?, ?, ?);
`, int64(1), int64(0), nil)

	testutils.AssertDebugStatementSql(t, stmt, `
INSERT INTO db.table3 (col1, col_int, col2)
VALUES (1, 0, NULL);
`)
}

func TestInsertModelsOption(t *testing.T) {
	stmt := table3.INSERT(table3Col1, table3ColInt, table3StrCol).
		MODELS([]optTable3{
			{Col1: 1, Col2: functional.Some("a")},
			{Col1: 2, ColInt: functional.Some[int64](2)},
		})

	assertStatementSql(t, stmt, `
INSERT INTO db.table3 (col1, col_int, col2)
VALUES (?, ?, ?),
       (?, ?, ?);
`, int64(1), nil, "a", int64(2), int64(2), nil)
}

func TestInsertValuesOption(t *testing.T) {
	stmt := table3.INSERT(table3Col1, table3ColInt, table3StrCol).
		VALUES(1, functional.Some(2), functional.None[string]())

	assertStatementSql(t, stmt, `
INSERT INTO db.table3 (col1, col_int, col2)
VALUES (?, ?, ?);
`, 1, 2, nil)
}

func TestUpdateModelOption(t *testing.T) {
	stmt := table3.UPDATE(table3ColInt, table3StrCol).
		MODEL(optTable3{ColInt: functional.None[int64](), Col2: functional.Some("b")}).
		WHERE(table3Col1.EQ(Int(1)))

	assertStatementSql(t, stmt, `
UPDATE db.table3
SET col_int = ?,
    col2 = ?
WHERE table3.col1 = ?;
`, nil, "b", int64(1))
}

func TestUpdateSetOption(t *testing.T) {
	stmt := table3.UPDATE(table3ColInt, table3StrCol).
		SET(functional.Some[int64](7), functional.None[string]()).
		WHERE(table3Col1.EQ(Int(1)))

	assertStatementSql(t, stmt, `
UPDATE db.table3
SET col_int = ?,
    col2 = ?
WHERE table3.col1 = ?;
`, int64(7), nil, int64(1))

	typed := table3.UPDATE().
		SET(
			table3StrCol.SET(StringOpt(functional.None[string]())),
			table3ColInt.SET(Int32Opt(functional.Some[int32](7))),
		).
		WHERE(table3Col1.EQ(Int(1)))

	testutils.AssertDebugStatementSql(t, typed, `
UPDATE db.table3
SET col2 = NULL,
    col_int = 7
WHERE table3.col1 = 1;
`)
}

func TestInsertOnDuplicateKeyOption(t *testing.T) {
	stmt := table3.INSERT(table3Col1, table3StrCol).
		VALUES(1, functional.None[string]()).
		ON_DUPLICATE_KEY_UPDATE(table3StrCol.SET(StringOpt(functional.Some("x"))))

	testutils.AssertDebugStatementSql(t, stmt, `
INSERT INTO db.table3 (col1, col2)
VALUES (1, NULL)
ON DUPLICATE KEY UPDATE col2 = 'x';
`)
}
