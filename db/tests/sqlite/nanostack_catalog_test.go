package sqlite

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv"
)

// TestCatalogMethodsMatchCatalog checks that the ColumnTypes and ForeignKeys
// methods that nanostack gen db writes match the catalogue, for every table of the
// test data (Nanostack harness, see testenv.CheckCatalog).
func TestCatalogMethodsMatchCatalog(t *testing.T) {
	testenv.CheckCatalog(t, currentEnv)
}
