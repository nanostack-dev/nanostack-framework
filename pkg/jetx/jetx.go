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

func ToStringExpressionSliceMap[T any](slice []T, f func(T) string) []jet.Expression {
	result := make([]jet.Expression, 0, len(slice))
	for _, value := range slice {
		result = append(result, jet.String(f(value)))
	}
	return result
}

func ToStringExpressions[T any](slice []T) []jet.Expression {
	result := make([]jet.Expression, len(slice))
	for i, value := range slice {
		result[i] = jet.String(fmt.Sprintf("%v", value))
	}
	return result
}

func OrderBy(column jet.Column, direction search.SortDirection) jet.OrderByClause {
	switch direction {
	case search.SortDescending:
		return column.DESC()
	default:
		return column.ASC()
	}
}

// FilterBuilder provides utilities for building Jet search filters.
type FilterBuilder struct{}

func NewFilterBuilder() FilterBuilder {
	return FilterBuilder{}
}

func (fb FilterBuilder) BuildIDFilter(column jet.ColumnString, ids []string) jet.BoolExpression {
	return fb.BuildStringArrayFilter(column, ids)
}

func (fb FilterBuilder) BuildStringArrayFilter(column jet.ColumnString, values []string) jet.BoolExpression {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 {
		return column.EQ(jet.String(values[0]))
	}
	return column.IN(ToStringExpressions(values)...)
}

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

func (fb FilterBuilder) CombineFilters(filters ...jet.BoolExpression) jet.BoolExpression {
	valid := compactBoolExpressions(filters)
	if len(valid) == 0 {
		return nil
	}
	result := valid[0]
	for i := 1; i < len(valid); i++ {
		result = result.AND(valid[i])
	}
	return result
}

func (fb FilterBuilder) CombineFiltersWithOr(filters ...jet.BoolExpression) jet.BoolExpression {
	valid := compactBoolExpressions(filters)
	if len(valid) == 0 {
		return nil
	}
	result := valid[0]
	for i := 1; i < len(valid); i++ {
		result = result.OR(valid[i])
	}
	return result
}

func compactBoolExpressions(filters []jet.BoolExpression) []jet.BoolExpression {
	valid := make([]jet.BoolExpression, 0, len(filters))
	for _, filter := range filters {
		if filter != nil {
			valid = append(valid, filter)
		}
	}
	return valid
}
