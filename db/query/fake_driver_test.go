package query_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
)

// script is what the fake database answers to every query or exec.
type script struct {
	columns      []string
	rows         [][]driver.Value
	err          error // returned by QueryContext/ExecContext
	rowsAffected int64
}

// openFake returns a *sql.DB backed by s: a minimal database/sql/driver fake, stdlib only.
func openFake(s script) *sql.DB {
	return sql.OpenDB(fakeConnector{s: s})
}

type fakeConnector struct{ s script }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{s: c.s}, nil }
func (c fakeConnector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fake: use the connector")
}

type fakeConn struct{ s script }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("fake: no prepare") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return fakeTx{}, nil }

func (c *fakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.s.err != nil {
		return nil, c.s.err
	}
	return &fakeRows{columns: c.s.columns, rows: c.s.rows}, nil
}

func (c *fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	if c.s.err != nil {
		return nil, c.s.err
	}
	return driver.RowsAffected(c.s.rowsAffected), nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	next    int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}
