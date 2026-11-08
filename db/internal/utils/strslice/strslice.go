// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package strslice

// Contains checks if slice of strings contains a string
func Contains(strings []string, contains string) bool {
	for _, str := range strings {
		if str == contains {
			return true
		}
	}

	return false
}
