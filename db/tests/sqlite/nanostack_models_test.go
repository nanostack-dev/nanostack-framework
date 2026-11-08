package sqlite

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv"
)

// TestGeneratedModelsUpToDate compares the committed models in db/tests/.gentestdata
// with the generator's output for the database under test (Nanostack harness). It
// keeps the models the suites compile against honest, and tests the generator.
func TestGeneratedModelsUpToDate(t *testing.T) {
	testenv.CheckModels(t, currentEnv)
}
