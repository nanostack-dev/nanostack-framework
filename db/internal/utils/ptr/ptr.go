// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package ptr

// Of returns the address of any given parameter
func Of[T any](value T) *T {
	return &value
}
