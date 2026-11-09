// Package keyset is the keyset pager of Nanostack DB (ADR 0001, ADR 0027).
//
// A Key reads one ordering column from a scanned row and compares that column
// with a cursor value. An Order is a total ordering: the requested sort keys,
// then a unique tiebreak. Order.Seek turns a cursor into the predicate for the
// rows after it, Order.Trim cuts a page fetched with LIMIT limit+1 and gives
// the next cursor, and Page runs the count and the page through db/query.
//
// The package takes the executor as a parameter (query.Executor). It never
// reads a transaction from a context: the caller resolves it. It supports
// PostgreSQL. A source of another dialect is ErrDialect.
//
// A cursor is position, not authority: see db/search.Cursor.
package keyset
