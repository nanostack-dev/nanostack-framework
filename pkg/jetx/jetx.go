// Package jetx holds the go-jet helpers that sit above query execution:
// ordering, expression conversion, and filter composition.
//
// Every Build* helper returns nil when it has nothing to filter on, so an unset
// search field contributes no predicate. CombineFilters and CombineFiltersWithOr
// drop those nils, which is what lets a caller pass every filter it can build
// and let the absent ones fall away.
//
// Query execution itself lives in pkg/db/transactor, which is the single entry
// point for running statements and translating constraint violations.
package jetx

import (
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// ToStringExpressionsFunc projects each value with toString and wraps the result
// as a bound SQL literal, ready to pass to IN. The returned slice is always
// non-nil, so an empty input yields no expressions rather than a nil slice.
func ToStringExpressionsFunc[T any](values []T, toString func(T) string) []jet.Expression {
	expressions := make([]jet.Expression, 0, len(values))
	for _, value := range values {
		expressions = append(expressions, jet.String(toString(value)))
	}
	return expressions
}

// ToStringExpressions is ToStringExpressionsFunc with the default projection:
// a string is taken as-is, anything else is rendered with %v. Reach for
// ToStringExpressionsFunc when a type needs more than its default formatting.
func ToStringExpressions[T any](values []T) []jet.Expression {
	return ToStringExpressionsFunc(values, func(value T) string {
		if s, ok := any(value).(string); ok {
			return s
		}
		return fmt.Sprintf("%v", value)
	})
}

// OrderBy builds the ORDER BY clause for a column. Anything other than an
// explicit descending direction sorts ascending, so an unset direction still
// produces a deterministic order.
func OrderBy(column jet.Column, direction search.SortDirection) jet.OrderByClause {
	switch direction {
	case search.SortDescending:
		return column.DESC()
	default:
		return column.ASC()
	}
}

// BuildIDFilter is BuildStringArrayFilter under the name a caller filtering on
// identifiers will look for.
func BuildIDFilter(column jet.ColumnString, ids []string) jet.BoolExpression {
	return BuildStringArrayFilter(column, ids)
}

// BuildStringArrayFilter matches column against values: a direct comparison for
// one value, IN for several, and no filter at all for none.
func BuildStringArrayFilter(column jet.ColumnString, values []string) jet.BoolExpression {
	switch len(values) {
	case 0:
		return nil
	case 1:
		return column.EQ(jet.String(values[0]))
	default:
		return column.IN(ToStringExpressions(values)...)
	}
}

// BuildSubstringFilter matches term as a substring of any of the columns with
// LIKE '%term%'. Use a tsvector column and a real full-text query when you need
// ranking or stemming.
func BuildSubstringFilter(columns []jet.ColumnString, term string) jet.BoolExpression {
	if term == "" || len(columns) == 0 {
		return nil
	}
	pattern := jet.String("%" + term + "%")
	conditions := make([]jet.BoolExpression, 0, len(columns))
	for _, column := range columns {
		conditions = append(conditions, column.LIKE(pattern))
	}
	return CombineFiltersWithOr(conditions...)
}

// BuildDateRangeFilter bounds column by from and to. Both bounds are inclusive.
func BuildDateRangeFilter(column jet.ColumnTimestampz, from, to *time.Time) jet.BoolExpression {
	var conditions []jet.BoolExpression
	if from != nil {
		conditions = append(conditions, column.GT_EQ(jet.TimestampzT(*from)))
	}
	if to != nil {
		conditions = append(conditions, column.LT_EQ(jet.TimestampzT(*to)))
	}
	return CombineFilters(conditions...)
}

// CombineFilters ANDs the filters that are present, left to right, and returns
// nil when none of them are.
func CombineFilters(filters ...jet.BoolExpression) jet.BoolExpression {
	var combined jet.BoolExpression
	for _, filter := range filters {
		switch {
		case filter == nil:
			continue
		case combined == nil:
			combined = filter
		default:
			combined = combined.AND(filter)
		}
	}
	return combined
}

// CombineFiltersWithOr is CombineFilters with OR.
func CombineFiltersWithOr(filters ...jet.BoolExpression) jet.BoolExpression {
	var combined jet.BoolExpression
	for _, filter := range filters {
		switch {
		case filter == nil:
			continue
		case combined == nil:
			combined = filter
		default:
			combined = combined.OR(filter)
		}
	}
	return combined
}
