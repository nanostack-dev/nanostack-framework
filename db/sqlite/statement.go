// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package sqlite

import (
	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
)

// RawStatement creates new sql statements from raw query and optional map of named arguments
func RawStatement(rawQuery string, namedArguments ...RawArgs) jet.SerializerStatement {
	return jet.RawStatement(Dialect, rawQuery, namedArguments...)
}
