// Package keyset is the keyset pager of Nanostack DB (ADR 0001, ADR 0027).
//
// A Key reads one ordering column from a scanned row and compares that column
// with a cursor value. An Order is a total ordering: the requested sort keys,
// then a unique tiebreak. Order.Seek turns a cursor into the predicate for the
// rows after it, Order.Trim cuts a page fetched with LIMIT limit+1 and gives
// the next cursor, and Page runs the count and the page through db/query.
//
// The package takes the executor as a parameter (query.Executor). It never
// reads a transaction from a context: the caller resolves it.
//
// Page runs on PostgreSQL, MySQL, MariaDB and SQLite with one contract: the
// same rows, every walk complete (no row skipped, no row repeated), and the
// same cursor tokens. Order and matching follow the column's collation. Page
// reads the dialect from its source and writes that dialect's SQL:
//
//   - NULLs sort last when ascending and first when descending, unless a key
//     asks for NullsFirst or NullsLast. MySQL and SQLite get an explicit
//     placement only for a nullable key whose placement differs from their
//     default, so a NOT NULL key stays a plain "col ASC" that an index serves.
//   - The seek is a row value on PostgreSQL and SQLite when every key is NOT
//     NULL and has one direction, and the expanded form otherwise. MySQL
//     always gets the expanded form: it does not range-scan a row value.
//   - Each codec refuses a cursor value that the dialect's column cannot hold
//     with search.ErrInvalidCursor, before any statement runs. A key whose
//     column type the dialect lacks is ErrDialect.
//
// Order.Seek and Order.OrderBy write PostgreSQL SQL, for a db/postgres
// statement that does not fit Page.
//
// A cursor is position, not authority: see db/search.Cursor.
package keyset
