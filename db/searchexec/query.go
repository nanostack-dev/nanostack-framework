package searchexec

import (
	"slices"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// Builder is the constraint of every generated builder Q: a struct whose only field is the
// embedded Query. It lets the methods of Query return the builder itself.
type Builder[T any, Q Builder[T, Q, S, P], S ~string, P comparable] interface {
	~struct{ Query[T, Q, S, P] }
}

// Query is the state of one search, embedded by its generated builder Q. It is immutable:
// every method returns a changed copy, so a search that is stored, reused or passed to
// search.All never changes under its owner.
//
// Only New makes a Query that runs. A struct literal of the builder has no spec, and Resolve
// refuses it with search.ErrUnscoped.
type Query[T any, Q Builder[T, Q, S, P], S ~string, P comparable] struct {
	spec  *Spec[T, S, P]
	scope P
	where []jet.BoolExpression
	sorts []search.Sort[S]
	page  search.Pagination
}

// New starts a search of spec for one tenant. scope is the generated scope value: one value,
// or the generated scope struct. The page size is the spec's default limit.
//
// Generated constructors call New. Resolve, Run and RunPage take the spec again and refuse a
// query that was built from another one, so a query built by hand on another spec never runs
// as this search.
func New[Q Builder[T, Q, S, P], T any, S ~string, P comparable](spec *Spec[T, S, P], scope P) Q {
	q := Query[T, Q, S, P]{spec: spec, scope: scope}
	if spec != nil {
		q.page.Limit = spec.limit
	}
	return Q{q}
}

// Scope returns the scope value the search was started with.
func (q Query[T, Q, S, P]) Scope() P {
	return q.scope
}

// SortBy adds a sort key after the keys already set. The tiebreak ends every order. Without
// a sort key, the spec's default sort applies. A field the search cannot sort by is
// search.ErrUnknownSort when the search runs.
func (q Query[T, Q, S, P]) SortBy(field S, direction search.SortDirection) Q {
	q.sorts = append(slices.Clip(q.sorts), search.Sort[S]{Field: field, Direction: direction})
	return Q{q}
}

// After continues the search after the NextCursor of a previous page. A cursor clears the
// offset, because it already says where the page starts.
func (q Query[T, Q, S, P]) After(cursor string) Q {
	q.page.Cursor = cursor
	if cursor != "" {
		q.page.Offset = 0
	}
	return Q{q}
}

// Offset skips n rows of the ordered result. A non-zero n clears the cursor; Offset(0)
// keeps it, as After("") keeps the offset, so a mapper can set both from one request. It
// serves the deprecated limit and offset lists; new callers continue with After. A negative
// n is search.ErrInvalidOffset when the search runs.
func (q Query[T, Q, S, P]) Offset(n int32) Q {
	q.page.Offset = n
	if n != 0 {
		q.page.Cursor = ""
	}
	return Q{q}
}

// Limit sets the page size. A size outside 1 to search.MaxLimit is search.ErrInvalidLimit
// when the search runs: Limit(0) is refused, never replaced by the default.
func (q Query[T, Q, S, P]) Limit(n int32) Q {
	q.page.Limit = n
	return Q{q}
}
