// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package metadata

// Enum metadata struct
type Enum struct {
	Name    string `sql:"primary_key"`
	Comment string
	Values  []string
}
