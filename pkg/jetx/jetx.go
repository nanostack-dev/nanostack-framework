// Package jetx holds the go-jet helpers that sit above query execution:
// ordering, expression conversion, and filter composition.
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

// ToStringExpressionSliceMap projects each item to a string and wraps it as a
// bound SQL literal, ready to pass to IN. The result is always non-nil, so an
// empty input yields no expressions rather than a nil slice.
func ToStringExpressionSliceMap[T any](slice []T, f func(T) string) []jet.Expression {
	result := make([]jet.Expression, 0, len(slice))
	for _, value := range slice {
		result = append(result, jet.String(f(value)))
	}
	return result
}

// ToStringExpressions is ToStringExpressionSliceMap with the default
// projection: each item is rendered with %v. Pass ToStringExpressionSliceMap a
// projection of your own when a type needs more than its default formatting.
func ToStringExpressions[T any](slice []T) []jet.Expression {
	return ToStringExpressionSliceMap(slice, func(value T) string {
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

// FilterBuilder provides utilities for building Jet search filters.
//
// Every Build* method returns nil when it has nothing to filter on, so an
// unset search field contributes no predicate. CombineFilters drops those nils,
// which is what lets a caller assemble a filter list unconditionally and let
// the absent ones fall away.
type FilterBuilder struct{}

// NewFilterBuilder returns a FilterBuilder. It carries no state.
func NewFilterBuilder() FilterBuilder {
	return FilterBuilder{}
}

// BuildIDFilter is BuildStringArrayFilter under the name a caller filtering on
// identifiers will look for.
func (fb FilterBuilder) BuildIDFilter(column jet.ColumnString, ids []string) jet.BoolExpression {
	return fb.BuildStringArrayFilter(column, ids)
}

// BuildStringArrayFilter matches column against values: a direct comparison for
// one value, IN for several, and no filter at all for none.
func (fb FilterBuilder) BuildStringArrayFilter(column jet.ColumnString, values []string) jet.BoolExpression {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 {
		return column.EQ(jet.String(values[0]))
	}
	return column.IN(ToStringExpressions(values)...)
}

// BuildFullTextSearchFilter matches term as a substring of any of the columns.
// The name is aspirational: this is a plain LIKE '%term%', not a Postgres
// full-text search.
func (fb FilterBuilder) BuildFullTextSearchFilter(columns []jet.ColumnString, term string) jet.BoolExpression {
	if term == "" || len(columns) == 0 {
		return nil
	}
	pattern := jet.String("%" + term + "%")
	conditions := make([]jet.BoolExpression, 0, len(columns))
	for _, column := range columns {
		conditions = append(conditions, column.LIKE(pattern))
	}
	return fb.CombineFiltersWithOr(conditions...)
}

// BuildDateRangeFilter bounds column by from and to, either of which may be
// nil to leave that end open.
func (fb FilterBuilder) BuildDateRangeFilter(column jet.ColumnTimestampz, from, to *time.Time) jet.BoolExpression {
	var conditions []jet.BoolExpression
	if from != nil {
		conditions = append(conditions, column.GT_EQ(jet.TimestampzT(*from)))
	}
	if to != nil {
		conditions = append(conditions, column.LT_EQ(jet.TimestampzT(*to)))
	}
	return fb.CombineFilters(conditions...)
}

// CombineFilters ANDs the filters that are present, and returns nil when none
// of them are.
func (fb FilterBuilder) CombineFilters(filters ...jet.BoolExpression) jet.BoolExpression {
	return combine(filters, jet.BoolExpression.AND)
}

// CombineFiltersWithOr ORs the filters that are present, and returns nil when
// none of them are.
func (fb FilterBuilder) CombineFiltersWithOr(filters ...jet.BoolExpression) jet.BoolExpression {
	return combine(filters, jet.BoolExpression.OR)
}

// combine folds the present filters left to right with join.
//
// join is the SQL operator as a function rather than a flag the body switches
// on: adding XOR would mean one more caller, not one more branch in here.
func combine(
	filters []jet.BoolExpression,
	join func(left, right jet.BoolExpression) jet.BoolExpression,
) jet.BoolExpression {
	present := presentFilters(filters)
	if len(present) == 0 {
		return nil
	}
	combined := present[0]
	for _, filter := range present[1:] {
		combined = join(combined, filter)
	}
	return combined
}

// presentFilters drops the nils that Build* helpers return for a search field
// the caller left unset.
func presentFilters(filters []jet.BoolExpression) []jet.BoolExpression {
	present := make([]jet.BoolExpression, 0, len(filters))
	for _, filter := range filters {
		if filter != nil {
			present = append(present, filter)
		}
	}
	return present
}
