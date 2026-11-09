package keyset_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
)

// answer is what the fake database returns to one query.
type answer struct {
	columns []string
	rows    [][]driver.Value
	err     error
}

// fakeDB is a database/sql fake that answers each query with the next answer
// and records the SQL it received. Standard library only.
type fakeDB struct {
	mu      sync.Mutex
	answers []answer
	queries []string
}

// open returns a *sql.DB that answers queries in order.
func (f *fakeDB) open() *sql.DB {
	return sql.OpenDB(fakeConnector{db: f})
}

func (f *fakeDB) next(query string) (answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, query)
	if len(f.answers) == 0 {
		return answer{}, errors.New("fake: no answer left for " + query)
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a, nil
}

func (f *fakeDB) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

// countAnswer answers a SELECT COUNT(*) with n.
func countAnswer(n int64) answer {
	return answer{columns: []string{"count"}, rows: [][]driver.Value{{n}}}
}

type fakeConnector struct{ db *fakeDB }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{db: c.db}, nil }
func (fakeConnector) Driver() driver.Driver                          { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fake: use the connector")
}

type fakeConn struct{ db *fakeDB }

func (*fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("fake: no prepare") }
func (*fakeConn) Close() error                        { return nil }
func (*fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("fake: no transactions") }

func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	a, err := c.db.next(query)
	if err != nil {
		return nil, err
	}
	if a.err != nil {
		return nil, a.err
	}
	return &fakeRows{columns: a.columns, rows: a.rows}, nil
}

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	next    int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (*fakeRows) Close() error        { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}
