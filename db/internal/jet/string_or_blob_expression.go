// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package jet

// StringOrBlobExpression is common interface for all string and blob expressions
type StringOrBlobExpression interface {
	Expression

	isStringOrBlob()
}
