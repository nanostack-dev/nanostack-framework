// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package sqlite

import (
	"testing"
)

func TestCAST(t *testing.T) {
	assertSerialize(t, CAST(Float(11.22)).AS("bigint"), `CAST(? AS bigint)`)
	assertSerialize(t, CAST(Int(22)).AS_TEXT(), `CAST(? AS TEXT)`)
	assertSerialize(t, CAST(Int(22)).AS_NUMERIC(), `CAST(? AS NUMERIC)`)
	assertSerialize(t, CAST(String("22")).AS_INTEGER(), `CAST(? AS INTEGER)`)
	assertSerialize(t, CAST(String("22.2")).AS_REAL(), `CAST(? AS REAL)`)
	assertSerialize(t, CAST(String("blob")).AS_BLOB(), `CAST(? AS BLOB)`)
}
