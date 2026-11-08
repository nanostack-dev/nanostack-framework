// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package jet

// NumericExpression is common interface for all integer and float expressions
type NumericExpression interface {
	Expression
	numericExpression
}

type numericExpression interface {
	isNumericExpression()
}

type numericExpressionImpl struct{}

func (n *numericExpressionImpl) isNumericExpression() {}
