// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package jet

// NewEnumValue creates new named enum value
func NewEnumValue(name string) StringExpression {
	return StringExp(newExpression(
		enumValueSerializer{name: name},
	))
}

type enumValueSerializer struct {
	name string
}

func (e enumValueSerializer) serialize(statement StatementType, out *SQLBuilder, options ...SerializeOption) {
	out.insertConstantArgument(e.name)
}
