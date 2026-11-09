package keyset

import "errors"

var (
	// ErrDialect reports a source that is not a table or join of db/postgres,
	// db/mysql or db/sqlite, or a key whose column type the source's dialect
	// lacks (TimeKey and InetKey off PostgreSQL, MySQLTimestampKey off MySQL).
	// Page reports it before any statement runs. It is a programming error,
	// not a client error.
	ErrDialect = errors.New("keyset: the dialect does not support this source or key")

	// ErrPageSourceMissing reports a page that runs without From.
	ErrPageSourceMissing = errors.New("keyset: paginated query has no FROM")

	// ErrPageOrderConflict reports a page that has both OrderBy and Keyset: a
	// keyset owns the whole ordering.
	ErrPageOrderConflict = errors.New("keyset: paginated query has both OrderBy and Keyset")
)
