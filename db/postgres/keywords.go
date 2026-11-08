// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package postgres

import "github.com/nanostack-dev/nanostack-framework/db/internal/jet"

const (
	// DEFAULT is jet equivalent of SQL DEFAULT
	DEFAULT = jet.DEFAULT
)

var (
	// NULL is jet equivalent of SQL NULL
	NULL = jet.NULL
	// STAR is jet equivalent of SQL *
	STAR = jet.STAR
	// PLUS_INFINITY is jet equivalent for sql infinity
	PLUS_INFINITY = jet.PLUS_INFINITY
	// MINUS_INFINITY is jet equivalent for sql -infinity
	MINUS_INFINITY = jet.MINUS_INFINITY
)
