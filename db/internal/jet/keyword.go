// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package jet

const (
	// DEFAULT is jet equivalent of SQL DEFAULT
	DEFAULT Keyword = "DEFAULT"
)

// Keyword type
type Keyword string

func (k Keyword) serialize(statement StatementType, out *SQLBuilder, options ...SerializeOption) {
	out.WriteString(string(k))
}
