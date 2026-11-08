// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package postgres

import "github.com/nanostack-dev/nanostack-framework/db/internal/jet"

// Interfaces for different postgres array column types
type (
	ColumnBoolArray       jet.ColumnArray[BoolExpression]
	ColumnStringArray     jet.ColumnArray[StringExpression]
	ColumnIntegerArray    jet.ColumnArray[IntegerExpression]
	ColumnFloatArray      jet.ColumnArray[FloatExpression]
	ColumnByteaArray      jet.ColumnArray[ByteaExpression]
	ColumnDateArray       jet.ColumnArray[DateExpression]
	ColumnTimestampArray  jet.ColumnArray[TimestampExpression]
	ColumnTimestampzArray jet.ColumnArray[TimestampzExpression]
	ColumnTimeArray       jet.ColumnArray[TimeExpression]
	ColumnTimezArray      jet.ColumnArray[TimezExpression]
	ColumnIntervalArray   jet.ColumnArray[IntervalExpression]
)

// Column constructors for different postgres array column types
var (
	BoolArrayColumn       = jet.ArrayColumn[BoolExpression]
	StringArrayColumn     = jet.ArrayColumn[StringExpression]
	IntegerArrayColumn    = jet.ArrayColumn[IntegerExpression]
	FloatArrayColumn      = jet.ArrayColumn[FloatExpression]
	ByteaArrayColumn      = jet.ArrayColumn[ByteaExpression]
	DateArrayColumn       = jet.ArrayColumn[DateExpression]
	TimestampArrayColumn  = jet.ArrayColumn[TimestampExpression]
	TimestampzArrayColumn = jet.ArrayColumn[TimestampzExpression]
	TimeArrayColumn       = jet.ArrayColumn[TimeExpression]
	TimezArrayColumn      = jet.ArrayColumn[TimezExpression]
	IntervalArrayColumn   = jet.ArrayColumn[IntervalExpression]
)
