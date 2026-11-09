package keyset

import "errors"

var (
	// ErrDialect reports a source that is not a table or join of db/postgres,
	// db/mysql or db/sqlite, or a key whose column type the source's dialect
	// lacks (TimeKey and InetKey off PostgreSQL, MySQLTimestampKey and
	// MariaDBTimestampKey off MySQL, RealKey on SQLite).
	// Page reports it before any statement runs. It is a programming error,
	// not a client error.
	ErrDialect = errors.New("keyset: the dialect does not support this source or key")

	// ErrNoProgress reports a page after a cursor whose last row is the cursor
	// row: the seek matched the row it starts after, and the next cursor would
	// repeat the page without end. The key does not match how the column
	// stores or compares its values (for example a SQLite time written in
	// another text form). Run returns it in place of the page. It is a
	// programming or data error, not a client error. It does not see a cycle
	// longer than one page, nor a skipped row.
	ErrNoProgress = errors.New("keyset: the page ends on its own cursor row")

	// ErrPageSourceMissing reports a page that runs without From.
	ErrPageSourceMissing = errors.New("keyset: paginated query has no FROM")

	// ErrPageOrderConflict reports a page that has both OrderBy and Keyset: a
	// keyset owns the whole ordering.
	ErrPageOrderConflict = errors.New("keyset: paginated query has both OrderBy and Keyset")
)
