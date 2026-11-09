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
//     NULL and has one direction, and no key is an enum on SQLite (SQLite
//     orders an enum through a CASE). The seek is the expanded form otherwise.
//     MySQL always gets the expanded form: it does not range-scan a row value.
//   - Each codec refuses a cursor value that the dialect's column cannot hold
//     with search.ErrInvalidCursor, before any statement runs. A key whose
//     column type the dialect lacks is ErrDialect.
//   - A key must match how the column stores and compares its values: see
//     LocalTimeKey for SQLite time text, and EnumKey for collations. A page
//     that ends on its own cursor row is ErrNoProgress, never a page that a
//     client would follow without end.
//
// Order.Seek and Order.OrderBy write PostgreSQL SQL, for a db/postgres
// statement that does not fit Page.
//
// A cursor is position, not authority: see db/search.Cursor.
//
// # Computed keys
//
// A computed key (IntExprKey, FloatExprKey, TimeExprKey, StringExprKey and
// their Nullable forms) orders on an expression, not on a column: for example
// a correlated count of a widget's requests, or the time of its last request.
//
// The first argument is the key's name: its name in the cursor signature and
// its select alias. It must be a row.field alias that matches
// ^[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*$, for example
// "widget_row.request_count" for the RequestCount field of a WidgetRow, or
// the constructor panics. The scan maps only this form to a field. The name
// must not be the name of another key of the search (OrderOf returns an
// error) or the alias of a selected column (Page returns an error).
//
// Page selects each computed key of the order AS its name (Order.Projections),
// so the scan fills the field that the next cursor reads. To fill the field on
// every order, put Key.Projection in the columns of the page: Page does not
// select it twice. A hand-written query that uses Order.OrderBy or Order.Seek
// selects Order.Projections itself. Select a computed value only through
// Key.Projection or Order.Projections, never with your own AS.
//
// An expression can be NULL even when it reads only NOT NULL columns: a
// scalar subquery that finds no row, such as the MAX of a widget with no
// requests. A non-nullable form reads NULL as the zero value of its type (0,
// the zero time.Time, the empty string): it wraps the expression in COALESCE,
// so the order, the seek and the field always agree. Use the Nullable form
// (with a pointer field) when NULL must sort apart from the zero value.
//
// The integer, float and time forms cast the expression to the SQL type of
// their cursor value, so the order, the seek and the select share one type and
// the scan always fits the field. The statement's dialect selects the cast
// when it is serialized:
//
//	           integer   float             time
//	PostgreSQL bigint    double precision  timestamp with time zone
//	MySQL      SIGNED    DOUBLE            (ErrDialect)
//	SQLite     INTEGER   REAL              (ErrDialect)
//
// PostgreSQL drops a cast to the type the expression already has. The text
// forms do not cast: a cast to text would change the order of a citext or
// collated expression. The time forms are PostgreSQL only, as TimeKey is:
// MySQL and SQLite have no timestamp with time zone, so a page with a time
// computed key and a source of another dialect is ErrDialect.
//
// The expression is SQL of the page's dialect: build a subquery with the
// SELECT of the dialect package of the page's source.
package keyset
