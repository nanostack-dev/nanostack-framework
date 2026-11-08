// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package jet

type expressionOrColumnList interface {
	Serializer
	isExpressionOrColumnList()
}

type columnListAssigment []ColumnAssigment

func (c columnListAssigment) isColumnAssignment() {}

func (c columnListAssigment) serialize(statement StatementType, out *SQLBuilder, options ...SerializeOption) {
	for i, columnAssigment := range c {
		if i > 0 {
			out.WriteString(",")
			out.NewLine()
		}

		columnAssigment.serialize(statement, out, options...)
	}
}
