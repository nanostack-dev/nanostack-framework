// This file is Nanostack-only (not in go-jet): an expression that each dialect selects
// when the statement is serialized. See db/UPSTREAM.md.

package jet

// DialectSwitch returns an expression whose SQL the statement's dialect selects. When the
// statement is serialized, choose is called with the dialect of the SQL builder, and the
// expression that it returns is serialized in its place, with the same options. Only that
// expression adds arguments to the statement.
//
// So one value serves every dialect. Packages outside the dialect packages cannot read the
// dialect at serialization time in any other way, because Serializer.serialize is unexported.
// Use it when the SQL itself differs per dialect (ILIKE against LOWER() LIKE, a row value
// against an expanded comparison, a dialect's cast). An operator that differs per dialect
// needs no switch: the dialect's OperatorSerializeOverride covers it.
//
// choose compares the dialect with the dialect packages' Dialect values (postgres.Dialect
// also serves CockroachDB, mysql.Dialect also serves MariaDB). It must return a non-nil
// expression for every dialect the statement can run on, also for an unknown dialect:
// debug strings of a bare expression use an unnamed default dialect. choose is called once
// per serialization and can be called from several goroutines at the same time, so it must
// not keep state.
//
// The switch serializes as a plain expression: in a projection it needs AS, also when it
// selects a column. Use the typed forms (BoolDialectSwitch, IntDialectSwitch, ...) to get
// the operators of a type.
//
// DialectSwitch panics if choose is nil. Serialization panics if choose returns nil.
func DialectSwitch(choose func(Dialect) Expression) Expression {
	if choose == nil {
		panic("jet: DialectSwitch needs a function")
	}

	return newExpression(dialectSwitch{choose: choose})
}

type dialectSwitch struct {
	choose func(Dialect) Expression
}

func (s dialectSwitch) serialize(statement StatementType, out *SQLBuilder, options ...SerializeOption) {
	selected := s.choose(out.Dialect)
	if selected == nil {
		panic("jet: DialectSwitch returned no expression for dialect " + out.Dialect.Name())
	}

	selected.serialize(statement, out, options...)
}

// typedSwitch adapts a typed choose function to DialectSwitch. A nil interface of the
// typed result stays a nil Expression, so the nil check of serialize sees it.
func typedSwitch[T Expression](choose func(Dialect) T) Expression {
	if choose == nil {
		panic("jet: DialectSwitch needs a function")
	}

	return DialectSwitch(func(d Dialect) Expression {
		selected := choose(d)
		if any(selected) == nil {
			return nil
		}

		return selected
	})
}

// BoolDialectSwitch is DialectSwitch for a bool expression.
func BoolDialectSwitch(choose func(Dialect) BoolExpression) BoolExpression {
	return BoolExp(typedSwitch(choose))
}

// IntDialectSwitch is DialectSwitch for an integer expression.
func IntDialectSwitch(choose func(Dialect) IntegerExpression) IntegerExpression {
	return IntExp(typedSwitch(choose))
}

// FloatDialectSwitch is DialectSwitch for a float expression.
func FloatDialectSwitch(choose func(Dialect) FloatExpression) FloatExpression {
	return FloatExp(typedSwitch(choose))
}

// StringDialectSwitch is DialectSwitch for a string expression.
func StringDialectSwitch(choose func(Dialect) StringExpression) StringExpression {
	return StringExp(typedSwitch(choose))
}

// DateDialectSwitch is DialectSwitch for a date expression.
func DateDialectSwitch(choose func(Dialect) DateExpression) DateExpression {
	return DateExp(typedSwitch(choose))
}

// TimeDialectSwitch is DialectSwitch for a time expression.
func TimeDialectSwitch(choose func(Dialect) TimeExpression) TimeExpression {
	return TimeExp(typedSwitch(choose))
}

// TimezDialectSwitch is DialectSwitch for a time with time zone expression.
func TimezDialectSwitch(choose func(Dialect) TimezExpression) TimezExpression {
	return TimezExp(typedSwitch(choose))
}

// TimestampDialectSwitch is DialectSwitch for a timestamp expression.
func TimestampDialectSwitch(choose func(Dialect) TimestampExpression) TimestampExpression {
	return TimestampExp(typedSwitch(choose))
}

// TimestampzDialectSwitch is DialectSwitch for a timestamp with time zone expression.
func TimestampzDialectSwitch(choose func(Dialect) TimestampzExpression) TimestampzExpression {
	return TimestampzExp(typedSwitch(choose))
}

// IntervalDialectSwitch is DialectSwitch for an interval expression.
func IntervalDialectSwitch(choose func(Dialect) IntervalExpression) IntervalExpression {
	return IntervalExp(typedSwitch(choose))
}

// BlobDialectSwitch is DialectSwitch for a blob expression.
func BlobDialectSwitch(choose func(Dialect) BlobExpression) BlobExpression {
	return BlobExp(typedSwitch(choose))
}
