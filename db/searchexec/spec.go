package searchexec

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// Spec is what nano gen repo knows about one search: the rows of type T it reads, its sort
// enum S and its scope type P. Its fields are unexported. Generated code builds one per
// search with DefineSpec, as a package variable, and passes that variable to New, Resolve,
// Run and RunPage. The runtime compares spec pointers, so a query runs only with the spec
// it was built from.
type Spec[T any, S ~string, P comparable] struct {
	from       func(P) keyset.Source
	columns    jet.Projection
	scope      []ScopeCol[P]
	context    []ContextScope
	unscoped   []string
	conditions []jet.BoolExpression
	keys       map[S]keyset.Key[T]
	tiebreak   []keyset.Key[T]
	sort       search.Sort[S]
	limit      int32
}

// SpecDef is the definition of a search that DefineSpec checks. nano gen repo writes it from
// the declaration.
type SpecDef[T any, S ~string, P comparable] struct {
	// From returns the table or the join the search reads. It gets the scope value, so a
	// tenant value can go into an ON clause. The source must be a db/postgres table or join;
	// any other source is keyset.ErrDialect when the search runs.
	From func(P) keyset.Source
	// Columns is the projection that fills a row of T: a table's AllColumns, or a
	// ProjectionList.
	Columns jet.Projection
	// Scope holds the tenant conditions that read the scope value (ScopeEq, ScopeOrNull,
	// ScopeNull).
	Scope []ScopeCol[P]
	// Context holds the tenant conditions that read the context (ScopeFunc).
	Context []ContextScope
	// Unscoped records each tenant column that the declaration exempts, as
	// "table.column: reason". It adds no condition and does not count as a scope.
	Unscoped []string
	// Conditions apply to every run, next to the scope.
	Conditions []jet.BoolExpression
	// Keys are the sortable fields.
	Keys map[S]keyset.Key[T]
	// Tiebreak makes the order total: a unique, NOT NULL set of columns, normally the
	// primary key.
	Tiebreak []keyset.Key[T]
	// Default is the sort when the caller sets none. A zero Default orders by the tiebreak
	// only.
	Default search.Sort[S]
	// Limit is the page size when the caller sets none, from 1 to search.MaxLimit.
	Limit int32
}

// DefineSpec checks d and returns its Spec. Generated code calls it in a package variable,
// so a wrong definition panics when the program starts, never during a request.
//
// DefineSpec panics when d has no From, no Columns, no key or no tiebreak, when it has no
// tenant condition (no Scope and no Context entry: an Unscoped entry is a record, not a
// scope), when a Scope entry is the zero ScopeCol, when a Context entry or a condition is
// nil, when the Default field is not a key, or when Limit is outside 1 to search.MaxLimit.
//
// The Spec holds copies of the slices and the map of d, so a later change to d does not
// change it.
func DefineSpec[T any, S ~string, P comparable](d SpecDef[T, S, P]) *Spec[T, S, P] {
	switch {
	case d.From == nil:
		panic("searchexec: DefineSpec: the search has no From")
	case d.Columns == nil:
		panic("searchexec: DefineSpec: the search has no Columns")
	case len(d.Keys) == 0:
		panic("searchexec: DefineSpec: the search has no sort key")
	case len(d.Tiebreak) == 0:
		panic("searchexec: DefineSpec: the search has no tiebreak")
	case len(d.Scope) == 0 && len(d.Context) == 0:
		panic("searchexec: DefineSpec: the search has no tenant scope (Scope or Context)")
	case d.Limit < 1 || d.Limit > search.MaxLimit:
		panic(fmt.Sprintf("searchexec: DefineSpec: the default limit %d is not between 1 and %d",
			d.Limit, search.MaxLimit))
	}
	for i, scope := range d.Scope {
		if scope.condition == nil {
			panic(
				fmt.Sprintf("searchexec: DefineSpec: Scope[%d] is not built with ScopeEq, ScopeOrNull or ScopeNull", i),
			)
		}
	}
	for i, scope := range d.Context {
		if scope == nil {
			panic(fmt.Sprintf("searchexec: DefineSpec: Context[%d] is nil", i))
		}
	}
	for i, condition := range d.Conditions {
		if condition == nil {
			panic(fmt.Sprintf("searchexec: DefineSpec: Conditions[%d] is nil", i))
		}
	}
	if d.Default.Field != "" {
		if _, ok := d.Keys[d.Default.Field]; !ok {
			panic(fmt.Sprintf("searchexec: DefineSpec: the default sort %q is not a key", d.Default.Field))
		}
	}
	return &Spec[T, S, P]{
		from:       d.From,
		columns:    d.Columns,
		scope:      slices.Clone(d.Scope),
		context:    slices.Clone(d.Context),
		unscoped:   slices.Clone(d.Unscoped),
		conditions: slices.Clone(d.Conditions),
		keys:       maps.Clone(d.Keys),
		tiebreak:   slices.Clone(d.Tiebreak),
		sort:       d.Default,
		limit:      d.Limit,
	}
}

// ScopeCol is one tenant condition that reads the scope value P. Build it with ScopeEq,
// ScopeOrNull or ScopeNull; the zero ScopeCol makes DefineSpec panic.
type ScopeCol[P comparable] struct {
	// condition returns the condition for the scope value, or search.ErrUnscoped when
	// the value it reads is zero.
	condition func(P) (jet.BoolExpression, error)
}

// ScopeEq keeps the rows where each column equals the value that value reads from the scope:
// col = $1 for one column, col1 = $1 AND col2 = $2 for several. One scope field can so
// scope a child and a parent column of the same name.
//
// A zero value ("", 0, uuid.Nil) refuses the search with search.ErrUnscoped. The error
// names the columns, not the value.
//
// ScopeEq panics when columns is empty or holds a nil column.
func ScopeEq[P comparable, V comparable](value func(P) V, columns ...jet.ColumnExpression) ScopeCol[P] {
	if value == nil {
		panic("searchexec: ScopeEq: value is nil")
	}
	names := columnNames("ScopeEq", columns)
	return ScopeCol[P]{condition: func(p P) (jet.BoolExpression, error) {
		v, err := nonZero(value(p), names)
		if err != nil {
			return nil, err
		}
		conditions := make([]jet.BoolExpression, len(columns))
		for i, column := range columns {
			conditions[i] = jet.Eq(column, jet.Literal(v))
		}
		return jet.AND(conditions...), nil
	}}
}

// ScopeOrNull keeps the rows where column equals the value that value reads from the scope,
// and the rows where column is NULL: (col = $1 OR col IS NULL). It serves rows that every
// tenant shares. A zero value refuses the search, as for ScopeEq.
//
// ScopeOrNull panics when column is nil.
func ScopeOrNull[P comparable, V comparable](value func(P) V, column jet.ColumnExpression) ScopeCol[P] {
	if value == nil {
		panic("searchexec: ScopeOrNull: value is nil")
	}
	names := columnNames("ScopeOrNull", []jet.ColumnExpression{column})
	return ScopeCol[P]{condition: func(p P) (jet.BoolExpression, error) {
		v, err := nonZero(value(p), names)
		if err != nil {
			return nil, err
		}
		return jet.OR(jet.Eq(column, jet.Literal(v)), column.IS_NULL()), nil
	}}
}

// ScopeNull keeps the rows where column is NULL: col IS NULL. It reads no scope value. It
// serves rows that no tenant owns.
//
// ScopeNull panics when column is nil.
func ScopeNull[P comparable](column jet.ColumnExpression) ScopeCol[P] {
	columnNames("ScopeNull", []jet.ColumnExpression{column})
	return ScopeCol[P]{condition: func(P) (jet.BoolExpression, error) {
		return column.IS_NULL(), nil
	}}
}

// Value is the accessor of a scope that is one value: ScopeEq(Value[string], column).
func Value[V comparable](v V) V { return v }

// ContextScope returns the tenant condition that the context carries, for example the
// workspace of the request. Resolve refuses a nil result with search.ErrUnscoped.
type ContextScope = func(context.Context) jet.BoolExpression

// ScopeFunc binds a context scope function to its column. fn is the function that the
// project registers for that column. It may return TRUE, for example for a read across the
// organization, but never nil.
//
// ScopeFunc panics when fn is nil.
func ScopeFunc[C any](fn func(context.Context, C) jet.BoolExpression, column C) ContextScope {
	if fn == nil {
		panic("searchexec: ScopeFunc: fn is nil")
	}
	return func(ctx context.Context) jet.BoolExpression { return fn(ctx, column) }
}

// Asc is an ascending sort of field, for SpecDef.Default.
func Asc[S ~string](field S) search.Sort[S] {
	return search.Sort[S]{Field: field, Direction: search.SortAscending}
}

// Desc is a descending sort of field, for SpecDef.Default.
func Desc[S ~string](field S) search.Sort[S] {
	return search.Sort[S]{Field: field, Direction: search.SortDescending}
}

// nonZero returns v, or search.ErrUnscoped naming the columns when v is the zero value.
func nonZero[V comparable](v V, columns string) (V, error) {
	var zero V
	if v == zero {
		return zero, fmt.Errorf("%w: the scope value of %s is zero", search.ErrUnscoped, columns)
	}
	return v, nil
}

// columnNames checks the columns of a scope constructor and names them for its errors.
func columnNames(constructor string, columns []jet.ColumnExpression) string {
	if len(columns) == 0 {
		panic("searchexec: " + constructor + ": no column")
	}
	names := ""
	for i, column := range columns {
		if column == nil {
			panic(fmt.Sprintf("searchexec: %s: column %d is nil", constructor, i))
		}
		if i > 0 {
			names += ", "
		}
		if table := column.TableName(); table != "" {
			names += table + "."
		}
		names += column.Name()
	}
	return names
}
