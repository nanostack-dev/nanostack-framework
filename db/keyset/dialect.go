package keyset

import (
	"fmt"

	ijet "github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// dialect is the SQL a page writes. PostgreSQL also serves CockroachDB, and
// MySQL also serves MariaDB.
type dialect uint8

const (
	dialectPostgres dialect = 1 << iota
	dialectMySQL
	dialectSQLite
)

func (d dialect) String() string {
	switch d {
	case dialectPostgres:
		return "PostgreSQL"
	case dialectMySQL:
		return "MySQL"
	case dialectSQLite:
		return "SQLite"
	}
	return fmt.Sprintf("dialect(%d)", uint8(d))
}

// dialects is a set of dialects.
type dialects uint8

func (s dialects) has(d dialect) bool { return s&dialects(d) != 0 }

// pageSource is a resolved Source: the dialect, and the statements a page
// runs in that dialect's builder.
type pageSource interface {
	dialect() dialect
	count(where ijet.BoolExpression) ijet.Statement
	// rows selects columns. offset < 0 writes no OFFSET.
	rows(
		columns []ijet.Projection, where ijet.BoolExpression, orderBy []ijet.OrderByClause, limit, offset int64,
	) ijet.Statement
}

// resolveSource resolves the table or join of a page. The dialect packages
// have no common table interface, so it switches on the three ReadableTable
// types.
func resolveSource(source Source) (pageSource, error) {
	switch table := source.(type) {
	case nil:
		return nil, ErrPageSourceMissing
	case postgres.ReadableTable:
		return postgresSource{table}, nil
	case mysql.ReadableTable:
		return mysqlSource{table}, nil
	case sqlite.ReadableTable:
		return sqliteSource{table}, nil
	default:
		return nil, fmt.Errorf(
			"%w: %T is not a table or join of db/postgres, db/mysql or db/sqlite", ErrDialect, source,
		)
	}
}

type postgresSource struct{ table postgres.ReadableTable }

func (postgresSource) dialect() dialect { return dialectPostgres }

func (s postgresSource) count(where ijet.BoolExpression) ijet.Statement {
	stmt := s.table.SELECT(postgres.COUNT(postgres.STAR))
	if where != nil {
		stmt = stmt.WHERE(where)
	}
	return stmt
}

func (s postgresSource) rows(
	columns []ijet.Projection, where ijet.BoolExpression, orderBy []ijet.OrderByClause, limit, offset int64,
) ijet.Statement {
	stmt := s.table.SELECT(columns[0], columns[1:]...)
	if where != nil {
		stmt = stmt.WHERE(where)
	}
	if len(orderBy) > 0 {
		stmt = stmt.ORDER_BY(orderBy...)
	}
	stmt = stmt.LIMIT(limit)
	if offset >= 0 {
		stmt = stmt.OFFSET(offset)
	}
	return stmt
}

type mysqlSource struct{ table mysql.ReadableTable }

func (mysqlSource) dialect() dialect { return dialectMySQL }

func (s mysqlSource) count(where ijet.BoolExpression) ijet.Statement {
	stmt := s.table.SELECT(mysql.COUNT(mysql.STAR))
	if where != nil {
		stmt = stmt.WHERE(where)
	}
	return stmt
}

func (s mysqlSource) rows(
	columns []ijet.Projection, where ijet.BoolExpression, orderBy []ijet.OrderByClause, limit, offset int64,
) ijet.Statement {
	stmt := s.table.SELECT(columns[0], columns[1:]...)
	if where != nil {
		stmt = stmt.WHERE(where)
	}
	if len(orderBy) > 0 {
		stmt = stmt.ORDER_BY(orderBy...)
	}
	stmt = stmt.LIMIT(limit)
	if offset >= 0 {
		stmt = stmt.OFFSET(offset)
	}
	return stmt
}

type sqliteSource struct{ table sqlite.ReadableTable }

func (sqliteSource) dialect() dialect { return dialectSQLite }

func (s sqliteSource) count(where ijet.BoolExpression) ijet.Statement {
	stmt := s.table.SELECT(sqlite.COUNT(sqlite.STAR))
	if where != nil {
		stmt = stmt.WHERE(where)
	}
	return stmt
}

func (s sqliteSource) rows(
	columns []ijet.Projection, where ijet.BoolExpression, orderBy []ijet.OrderByClause, limit, offset int64,
) ijet.Statement {
	stmt := s.table.SELECT(columns[0], columns[1:]...)
	if where != nil {
		stmt = stmt.WHERE(where)
	}
	if len(orderBy) > 0 {
		stmt = stmt.ORDER_BY(orderBy...)
	}
	stmt = stmt.LIMIT(limit)
	if offset >= 0 {
		stmt = stmt.OFFSET(offset)
	}
	return stmt
}
