// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package postgres

import "testing"

func TestLATERAL(t *testing.T) {
	assertSerialize(t,
		LATERAL(
			SELECT(Int(1)),
		).AS("lat1"),

		`LATERAL (
     SELECT $1
) AS lat1`)
}
