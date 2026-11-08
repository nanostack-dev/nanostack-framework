// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package mysql

import (
	"testing"

	"github.com/google/uuid"
)

func TestUUIDToBin(t *testing.T) {
	assertSerialize(t, UUID_TO_BIN(String(uuid.Nil.String())), `uuid_to_bin(?)`, uuid.Nil.String())
}
