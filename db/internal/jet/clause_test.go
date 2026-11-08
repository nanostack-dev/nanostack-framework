// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package jet

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestClauseSelect_Serialize(t *testing.T) {
	defer func() {
		r := recover()
		require.Equal(t, r, "jet: SELECT clause has to have at least one projection")
	}()

	selectClause := &ClauseSelect{}
	selectClause.Serialize(SelectStatementType, &SQLBuilder{})
}
