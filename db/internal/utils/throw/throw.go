// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package throw

// OnError will panic if err is not nill
func OnError(err error) {
	if err != nil {
		panic(err)
	}
}
