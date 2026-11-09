package keyset

import "errors"

var (
	// ErrDialect reports a key or a source that this dialect does not support.
	// It is a programming error, not a client error.
	ErrDialect = errors.New("keyset: the dialect does not support this source or key")

	// ErrPageSourceMissing reports a page that runs without From.
	ErrPageSourceMissing = errors.New("keyset: paginated query has no FROM")

	// ErrPageOrderConflict reports a page that has both OrderBy and Keyset: a
	// keyset owns the whole ordering.
	ErrPageOrderConflict = errors.New("keyset: paginated query has both OrderBy and Keyset")
)
