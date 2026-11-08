// Package filter builds WHERE conditions from optional inputs. A nil BoolExpression
// means "no condition". It is dropped by All and Any, and WHERE(nil) on a SELECT writes
// no WHERE clause.
//
//	stmt := SELECT(Invoice.AllColumns).FROM(Invoice).WHERE(filter.All(
//		Invoice.TenantID.EQ(UUID(f.Tenant)),
//		filter.When(f.Status, func(s string) BoolExpression { return Invoice.Status.EQ(String(s)) }),
//		filter.When(f.From, func(t time.Time) BoolExpression { return Invoice.CreatedAt.GT_EQ(TimestampzT(t)) }),
//	))
//
// UPDATE and DELETE keep their mandatory WHERE: when every condition is absent, All
// returns nil and the statement panics with "WHERE clause not set" instead of changing
// every row. Always include a non-optional key predicate in writes.
//
// Upstream AND, OR and WHERE are unchanged (AND() with no argument still panics).
//
// This package is Nanostack-only (not in go-jet). See db/UPSTREAM.md.
package filter

import (
	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// BoolExpression is the condition type of every dialect.
type BoolExpression = jet.BoolExpression

// When returns cond(v) when o carries v, and nil when o is absent.
func When[T any](o functional.Option[T], cond func(T) BoolExpression) BoolExpression {
	v, ok := o.Get()
	if !ok {
		return nil
	}
	return cond(v)
}

// If returns cond() when ok is true, and nil otherwise. cond is called only when ok is
// true, so it may build expressions that would be invalid otherwise (an IN over an
// empty list).
func If(ok bool, cond func() BoolExpression) BoolExpression {
	if !ok {
		return nil
	}
	return cond()
}

// All ANDs the non-nil conditions. Zero conditions give nil. One condition is returned
// unchanged. Two or more give jet's AND of them.
func All(conds ...BoolExpression) BoolExpression {
	return combine(jet.AND, conds)
}

// Any ORs the non-nil conditions. Zero conditions give nil, which means "no
// restriction" (not FALSE).
func Any(conds ...BoolExpression) BoolExpression {
	return combine(jet.OR, conds)
}

func combine(op func(...BoolExpression) BoolExpression, conds []BoolExpression) BoolExpression {
	var present []BoolExpression
	for _, c := range conds {
		if c != nil {
			present = append(present, c)
		}
	}
	switch len(present) {
	case 0:
		return nil
	case 1:
		return present[0]
	default:
		return op(present...)
	}
}
