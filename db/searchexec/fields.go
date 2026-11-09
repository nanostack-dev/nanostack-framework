package searchexec

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// The field types of a generated builder. A generated filter method returns one field of
// the query it is called on, and the method of the field returns the filtered copy:
//
//	q.Method().In("GET", "POST")
//	q.ReceivedAt().Since(t)
//
// A field has only the operators that its declaration names, so q.ReceivedAt().Contains(...)
// does not compile. Each New* constructor binds a field to a query; generated code calls it.
//
// A field never returns an error. A value over a limit (more than search.MaxInValues
// values, a term longer than search.MaxTermLength characters) records the error on the
// query, and Resolve, Run and RunPage return it. A field of a builder struct literal returns
// the zero builder, which Resolve refuses with search.ErrUnscoped.
//
// Each New* constructor panics when an expression or a literal function is nil: the
// generated code is wrong whatever the request.

// binding is what a field keeps of its query: a function that adds a condition, and one
// that records an error. The zero binding belongs to a field of a struct literal.
type binding[Q any] struct {
	with func(jet.BoolExpression) Q
	fail func(error) Q
}

// bind binds a field to q. A query with no spec (a struct literal of the builder) gets the
// zero binding.
func bind[T any, Q Builder[T, Q, S, P], S ~string, P comparable](q Query[T, Q, S, P]) binding[Q] {
	if q.spec == nil {
		return binding[Q]{}
	}
	return binding[Q]{with: q.with, fail: q.fail}
}

// add returns the query with the condition that build returns. On the zero binding it
// returns the zero builder and does not call build.
func (b binding[Q]) add(build func() jet.BoolExpression) Q {
	if b.with == nil {
		var zero Q
		return zero
	}
	return b.with(build())
}

// refuse returns the query with err recorded. On the zero binding it returns the zero
// builder.
func (b binding[Q]) refuse(err error) Q {
	if b.fail == nil {
		var zero Q
		return zero
	}
	return b.fail(err)
}

// In is a field that keeps the rows whose value is one of a list.
type In[Q, V any] struct {
	b    binding[Q]
	expr jet.Expression
	lit  func(V) jet.Expression
}

// NewIn binds an In field to q: expr IN (lit(v1), lit(v2), ...).
func NewIn[T any, Q Builder[T, Q, S, P], S ~string, P comparable, V any, E jet.Expression](
	q Query[T, Q, S, P], expr jet.Expression, lit func(V) E,
) In[Q, V] {
	mustExpr("NewIn", expr)
	return In[Q, V]{b: bind(q), expr: expr, lit: widen(mustLit("NewIn", lit))}
}

// In keeps the rows whose value is one of vs. No values match no row: a Go caller that
// builds an empty list gets nothing, not everything. More than search.MaxInValues values
// records search.ErrTooManyValues.
func (f In[Q, V]) In(vs ...V) Q {
	if err := checkValues(vs, f.expr); err != nil {
		return f.b.refuse(err)
	}
	return f.b.add(func() jet.BoolExpression { return in(f.expr, vs, f.lit) })
}

// InFold is a field that keeps the rows whose text value is one of a list, without case.
type InFold[Q, V any] struct {
	b    binding[Q]
	expr jet.StringExpression
	lit  func(V) jet.StringExpression
}

// NewInFold binds an InFold field to q: LOWER(expr) IN (LOWER(lit(v1)), ...). The database
// folds both sides by the rule of the column's collation, so the answer agrees with the
// column also for letters where Go and the database fold differently (the Turkish dotted
// I, the final sigma). expr must be a text expression.
func NewInFold[T any, Q Builder[T, Q, S, P], S ~string, P comparable, V any, E jet.StringExpression](
	q Query[T, Q, S, P], expr jet.StringExpression, lit func(V) E,
) InFold[Q, V] {
	mustExpr("NewInFold", expr)
	lit = mustLit("NewInFold", lit)
	return InFold[Q, V]{b: bind(q), expr: expr, lit: func(v V) jet.StringExpression { return lit(v) }}
}

// In keeps the rows whose value is one of vs without case. No values match no row. More
// than search.MaxInValues values records search.ErrTooManyValues.
func (f InFold[Q, V]) In(vs ...V) Q {
	if err := checkValues(vs, f.expr); err != nil {
		return f.b.refuse(err)
	}
	return f.b.add(func() jet.BoolExpression { return inFold(f.expr, vs, f.lit) })
}

// Eq is a field that keeps the rows whose value equals one value.
type Eq[Q, V any] struct {
	b    binding[Q]
	expr jet.Expression
	lit  func(V) jet.Expression
}

// NewEq binds an Eq field to q: expr = lit(v).
func NewEq[T any, Q Builder[T, Q, S, P], S ~string, P comparable, V any, E jet.Expression](
	q Query[T, Q, S, P], expr jet.Expression, lit func(V) E,
) Eq[Q, V] {
	mustExpr("NewEq", expr)
	return Eq[Q, V]{b: bind(q), expr: expr, lit: widen(mustLit("NewEq", lit))}
}

// Eq keeps the rows whose value equals v.
func (f Eq[Q, V]) Eq(v V) Q {
	return f.b.add(func() jet.BoolExpression { return jet.Eq(f.expr, f.lit(v)) })
}

// EqIn is a field with both Eq and In.
type EqIn[Q, V any] struct {
	eq Eq[Q, V]
	in In[Q, V]
}

// NewEqIn binds an EqIn field to q.
func NewEqIn[T any, Q Builder[T, Q, S, P], S ~string, P comparable, V any, E jet.Expression](
	q Query[T, Q, S, P], expr jet.Expression, lit func(V) E,
) EqIn[Q, V] {
	return EqIn[Q, V]{eq: NewEq(q, expr, lit), in: NewIn(q, expr, lit)}
}

// Eq keeps the rows whose value equals v, as Eq.Eq does.
func (f EqIn[Q, V]) Eq(v V) Q { return f.eq.Eq(v) }

// In keeps the rows whose value is one of vs, as In.In does.
func (f EqIn[Q, V]) In(vs ...V) Q { return f.in.In(vs...) }

// TimeRange is a field that keeps the rows whose time is on one side of a bound. Each call
// adds one bound; two calls make a range.
type TimeRange[Q any] struct {
	b    binding[Q]
	expr jet.TimestampzExpression
}

// NewTimeRange binds a TimeRange field to q. expr is a timestamp with time zone.
func NewTimeRange[T any, Q Builder[T, Q, S, P], S ~string, P comparable](
	q Query[T, Q, S, P], expr jet.TimestampzExpression,
) TimeRange[Q] {
	mustExpr("NewTimeRange", expr)
	return TimeRange[Q]{b: bind(q), expr: expr}
}

// Since keeps the rows at t or later: expr >= t.
func (f TimeRange[Q]) Since(t time.Time) Q {
	return f.b.add(func() jet.BoolExpression { return f.expr.GT_EQ(jet.TimestampzT(t)) })
}

// Until keeps the rows at t or earlier: expr <= t.
func (f TimeRange[Q]) Until(t time.Time) Q {
	return f.b.add(func() jet.BoolExpression { return f.expr.LT_EQ(jet.TimestampzT(t)) })
}

// After keeps the rows later than t: expr > t.
func (f TimeRange[Q]) After(t time.Time) Q {
	return f.b.add(func() jet.BoolExpression { return f.expr.GT(jet.TimestampzT(t)) })
}

// Before keeps the rows earlier than t: expr < t.
func (f TimeRange[Q]) Before(t time.Time) Q {
	return f.b.add(func() jet.BoolExpression { return f.expr.LT(jet.TimestampzT(t)) })
}

// Presence is a field that keeps the rows where a nullable value is set, or not set.
type Presence[Q any] struct {
	b    binding[Q]
	expr jet.Expression
}

// NewPresence binds a Presence field to q.
func NewPresence[T any, Q Builder[T, Q, S, P], S ~string, P comparable](
	q Query[T, Q, S, P], expr jet.Expression,
) Presence[Q] {
	mustExpr("NewPresence", expr)
	return Presence[Q]{b: bind(q), expr: expr}
}

// Present keeps the rows where the value is set (expr IS NOT NULL) when present is true,
// and the rows where it is NULL (expr IS NULL) when present is false.
func (f Presence[Q]) Present(present bool) Q {
	if present {
		return f.b.add(func() jet.BoolExpression { return f.expr.IS_NOT_NULL() })
	}
	return f.b.add(func() jet.BoolExpression { return f.expr.IS_NULL() })
}

// Flag is a field that applies a named condition, or its opposite.
type Flag[Q any] struct {
	b         binding[Q]
	condition jet.BoolExpression
}

// NewFlag binds a Flag field to q.
func NewFlag[T any, Q Builder[T, Q, S, P], S ~string, P comparable](
	q Query[T, Q, S, P], condition jet.BoolExpression,
) Flag[Q] {
	mustExpr("NewFlag", condition)
	return Flag[Q]{b: bind(q), condition: condition}
}

// Is keeps the rows where the condition is true when want is true, and the other rows when
// want is false: condition IS NOT TRUE, so a row where the condition is NULL counts as a
// row where it does not hold. Is(true) and Is(false) together return every row once.
func (f Flag[Q]) Is(want bool) Q {
	if want {
		return f.b.add(func() jet.BoolExpression { return f.condition })
	}
	return f.b.add(func() jet.BoolExpression { return f.condition.IS_NOT_TRUE() })
}

// Text is a field that keeps the rows where text expressions contain a term.
type Text[Q any] struct {
	b     binding[Q]
	exprs []jet.StringExpression
}

// NewText binds a Text field to q. Each term matches each of exprs, as Contains does.
//
// NewText panics when exprs is empty or holds a nil expression.
func NewText[T any, Q Builder[T, Q, S, P], S ~string, P comparable](
	q Query[T, Q, S, P], exprs ...jet.StringExpression,
) Text[Q] {
	if len(exprs) == 0 {
		panic("searchexec: NewText: no expression")
	}
	for i, expr := range exprs {
		if expr == nil {
			panic(fmt.Sprintf("searchexec: NewText: expression %d is nil", i))
		}
	}
	return Text[Q]{b: bind(q), exprs: append([]jet.StringExpression(nil), exprs...)}
}

// Contains keeps the rows where at least one expression contains term, by the rule of the
// function Contains: a blank term filters nothing. A term longer than search.MaxTermLength
// characters (after the trim) records search.ErrTermTooLong.
func (f Text[Q]) Contains(term string) Q {
	if err := checkTerm(term); err != nil {
		return f.b.refuse(err)
	}
	return f.b.add(func() jet.BoolExpression { return Contains(term, f.exprs...) })
}

// ContainsAny keeps the rows where at least one expression contains at least one of terms,
// by the rule of the function ContainsAny. No terms match no row; a blank term among them
// matches every row. More than search.MaxInValues terms records search.ErrTooManyValues,
// and a term longer than search.MaxTermLength characters records search.ErrTermTooLong.
func (f Text[Q]) ContainsAny(terms ...string) Q {
	if err := checkValues(terms, firstOf(f.exprs)); err != nil {
		return f.b.refuse(err)
	}
	for _, term := range terms {
		if err := checkTerm(term); err != nil {
			return f.b.refuse(err)
		}
	}
	return f.b.add(func() jet.BoolExpression { return ContainsAny(terms, f.exprs...) })
}

// Exists is a field that keeps the rows that have a child row whose value is one of a list.
type Exists[Q, V any] struct {
	b           binding[Q]
	child       keyset.Source
	correlation jet.BoolExpression
	match       func([]V) jet.BoolExpression
	expr        jet.Expression
}

// NewExists binds an Exists field to q:
//
//	EXISTS (SELECT 1 FROM child WHERE correlation AND column IN (lit(v1), ...))
//
// correlation ties the child row to the row of the search: the link column of child equals
// the key that it references, and each tenant column of child equals a covered tenant
// column or a scope value. nano gen repo writes it from the declared foreign key and checks
// the tenant columns. child must be a db/postgres table; a child of another dialect records
// keyset.ErrDialect.
func NewExists[T any, Q Builder[T, Q, S, P], S ~string, P comparable, V any, E jet.Expression](
	q Query[T, Q, S, P], child keyset.Source, correlation jet.BoolExpression, column jet.Expression, lit func(V) E,
) Exists[Q, V] {
	mustExists("NewExists", child, correlation, column)
	l := widen(mustLit("NewExists", lit))
	return Exists[Q, V]{
		b: bind(q), child: child, correlation: correlation, expr: column,
		match: func(vs []V) jet.BoolExpression { return in(column, vs, l) },
	}
}

// NewExistsFold is NewExists with the case folded on both sides, as InFold does:
// LOWER(column) IN (LOWER(lit(v1)), ...). column must be a text column of child.
func NewExistsFold[T any, Q Builder[T, Q, S, P], S ~string, P comparable, V any, E jet.StringExpression](
	q Query[T, Q, S, P], child keyset.Source, correlation jet.BoolExpression, column jet.StringExpression,
	lit func(V) E,
) Exists[Q, V] {
	mustExists("NewExistsFold", child, correlation, column)
	lit = mustLit("NewExistsFold", lit)
	l := func(v V) jet.StringExpression { return lit(v) }
	return Exists[Q, V]{
		b: bind(q), child: child, correlation: correlation, expr: column,
		match: func(vs []V) jet.BoolExpression { return inFold(column, vs, l) },
	}
}

// In keeps the rows that have a child row whose value is one of vs. No values match no row.
// More than search.MaxInValues values records search.ErrTooManyValues.
func (f Exists[Q, V]) In(vs ...V) Q {
	if f.b.with == nil {
		var zero Q
		return zero
	}
	if err := checkValues(vs, f.expr); err != nil {
		return f.b.refuse(err)
	}
	child, ok := f.child.(postgres.ReadableTable)
	if !ok {
		return f.b.refuse(
			fmt.Errorf("%w: the search runtime supports PostgreSQL only; the child %T is not a PostgreSQL table",
				keyset.ErrDialect, f.child),
		)
	}
	if len(vs) == 0 {
		return f.b.add(sqlFalse)
	}
	return f.b.add(func() jet.BoolExpression {
		return jet.EXISTS(
			postgres.SELECT(jet.AtomicCustomExpression(jet.Token("1"))).
				FROM(child).
				WHERE(jet.AND(f.correlation, f.match(vs))),
		)
	})
}

// StringLit is the literal of a string or of a named string type, for a text column.
func StringLit[V ~string](v V) jet.StringExpression { return jet.String(string(v)) }

// BoolLit is the literal of a bool or of a named bool type.
func BoolLit[V ~bool](v V) jet.BoolExpression { return jet.Bool(bool(v)) }

// IntLit is the literal of an integer, for an integer column.
func IntLit[V ~int | ~int32 | ~int64](v V) jet.IntegerExpression { return jet.Int(int64(v)) }

// UUIDLit is the literal of a UUID. PostgreSQL gets a uuid (CAST($1 AS uuid)), so it
// compares with a uuid column; MySQL, MariaDB and SQLite get the lower-case canonical text,
// as their UUID columns store it.
func UUIDLit(v uuid.UUID) jet.StringExpression {
	return jet.StringDialectSwitch(func(d jet.Dialect) jet.StringExpression {
		if d == postgres.Dialect {
			return postgres.UUID(v)
		}
		return jet.String(v.String())
	})
}

// in is expr IN (lit(v)...), or FALSE for no values.
func in[V any](expr jet.Expression, vs []V, lit func(V) jet.Expression) jet.BoolExpression {
	if len(vs) == 0 {
		return sqlFalse()
	}
	literals := make([]jet.Expression, len(vs))
	for i, v := range vs {
		literals[i] = lit(v)
	}
	return expr.IN(literals...)
}

// inFold is LOWER(expr) IN (LOWER(lit(v))...), or FALSE for no values.
func inFold[V any](expr jet.StringExpression, vs []V, lit func(V) jet.StringExpression) jet.BoolExpression {
	if len(vs) == 0 {
		return sqlFalse()
	}
	literals := make([]jet.Expression, len(vs))
	for i, v := range vs {
		literals[i] = jet.LOWER(lit(v))
	}
	return jet.LOWER(expr).IN(literals...)
}

// checkValues refuses more than search.MaxInValues values. The error names the expression
// and the count, never a value.
func checkValues[V any](vs []V, expr jet.Expression) error {
	if len(vs) > search.MaxInValues {
		return fmt.Errorf("%w: %s has %d values, the limit is %d",
			search.ErrTooManyValues, exprName(expr), len(vs), search.MaxInValues)
	}
	return nil
}

// checkTerm refuses a term longer than search.MaxTermLength characters after the trim, the
// term that Contains matches. The error gives the length, never the term.
func checkTerm(term string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(term)); n > search.MaxTermLength {
		return fmt.Errorf("%w: a term of %d characters, the limit is %d",
			search.ErrTermTooLong, n, search.MaxTermLength)
	}
	return nil
}

// exprName names a column as table.column, and any other expression as "a filter".
func exprName(expr jet.Expression) string {
	column, ok := expr.(jet.Column)
	if !ok {
		return "a filter"
	}
	if table := column.TableName(); table != "" {
		return table + "." + column.Name()
	}
	return column.Name()
}

// firstOf returns the first expression of exprs, or nil.
func firstOf(exprs []jet.StringExpression) jet.Expression {
	if len(exprs) == 0 {
		return nil
	}
	return exprs[0]
}

// widen returns lit as a function that returns a jet.Expression.
func widen[V any, E jet.Expression](lit func(V) E) func(V) jet.Expression {
	return func(v V) jet.Expression { return lit(v) }
}

func mustExpr(constructor string, expr jet.Expression) {
	if expr == nil {
		panic("searchexec: " + constructor + ": the expression is nil")
	}
}

func mustLit[V, E any](constructor string, lit func(V) E) func(V) E {
	if lit == nil {
		panic("searchexec: " + constructor + ": the literal function is nil")
	}
	return lit
}

func mustExists(constructor string, child keyset.Source, correlation jet.BoolExpression, column jet.Expression) {
	switch {
	case child == nil:
		panic("searchexec: " + constructor + ": the child is nil")
	case correlation == nil:
		panic("searchexec: " + constructor + ": the correlation is nil")
	}
	mustExpr(constructor, column)
}
