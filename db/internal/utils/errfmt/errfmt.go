// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package errfmt

import (
	"github.com/nanostack-dev/nanostack-framework/db/internal/utils/is"
	"strings"
)

// Trace returns well formatted wrapped error trace string
func Trace(err error) string {
	if is.Nil(err) {
		return ""
	}
	return "Error trace:\n" + " - " + strings.Replace(err.Error(), ": ", ":\n - ", -1)
}
