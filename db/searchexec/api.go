package searchexec

import "github.com/nanostack-dev/nanostack-framework/db/search"

// The API helpers. The generated <Name>SearchFromAPI calls them to turn an API request body
// into a builder. They apply the rules of the API contract, which differ from the rules of a
// Go caller: in the API, a property that is not sent filters nothing. They do no checks of
// their own. Resolve, Run and RunPage refuse what the builder refuses: an unknown sort, a
// page outside the limits, too many values.

// Some applies set when vs is not empty, and returns q unchanged when it is. In the API an
// empty or missing list filters nothing. From Go, a field's In with no values matches no row.
//
//	q = searchexec.Some(q, q.Method().In, f.Methods)
func Some[Q, V any](q Q, set func(...V) Q, vs []V) Q {
	if len(vs) == 0 {
		return q
	}
	return set(vs...)
}

// Opt applies set to *v when v is not nil, and returns q unchanged when it is. In the API a
// property that is not sent filters nothing.
//
//	q = searchexec.Opt(q, q.ReceivedAt().Since, f.Since)
func Opt[Q, V any](q Q, set func(V) Q, v *V) Q {
	if v == nil {
		return q
	}
	return set(*v)
}

// OrZero returns *body, or the zero B when body is nil. A request that sends no body, or no
// filter object, then reads as one that sends every property empty.
func OrZero[B any](body *B) B {
	if body == nil {
		var zero B
		return zero
	}
	return *body
}

// APISort applies the sort_by and sort_direction properties of an API request:
//
//   - neither: q is unchanged, and the spec's default sort applies with its own direction;
//   - sort_by alone: the field, DESC;
//   - sort_direction alone: def, the field of the default sort, in that direction;
//   - both: the field in that direction.
//
// sortBy is the builder's SortBy. A is the API's sort enum and D its direction enum (the
// x-go-type of the body), converted by their string values. nano gen repo checks at
// generation time that the values of A are the sorts of the search; Run refuses another
// value with search.ErrUnknownSort.
func APISort[Q any, S ~string, A ~string, D ~string](
	q Q, sortBy func(S, search.SortDirection) Q, field *A, direction *D, def S,
) Q {
	if field == nil {
		return APIDirection(q, sortBy, def, direction)
	}
	dir := search.SortDescending
	if direction != nil {
		dir = search.SortDirection(*direction)
	}
	return sortBy(S(*field), dir)
}

// APIDirection applies the sort_direction property of an API request that has no sort_by:
// the direction applies to def, the field of the default sort. A nil direction returns q
// unchanged, so the spec's default sort applies with its own direction.
func APIDirection[Q any, S ~string, D ~string](
	q Q, sortBy func(S, search.SortDirection) Q, def S, direction *D,
) Q {
	if direction == nil {
		return q
	}
	return sortBy(def, search.SortDirection(*direction))
}

// APIPage applies the pagination object of an API request. A nil object keeps the page of
// q. A zero limit is a limit that the request did not send: it keeps the limit of q, which is
// the spec's default limit (nano gen repo checks it against the API's default). Every other
// limit, the offset and the cursor are taken as sent, also together, so Resolve refuses
// what the client sent wrong (search.ErrInvalidLimit, search.ErrInvalidOffset,
// search.ErrCursorWithOffset, search.ErrInvalidCursor).
//
// APIPage is not a method of the builder. A Go caller sets the page with Limit, After and
// Offset, and Limit(0) is refused, never replaced by the default.
func APIPage[T any, Q Builder[T, Q, S, P], S ~string, P comparable](q Query[T, Q, S, P], p *search.Pagination) Q {
	if p != nil {
		if p.Limit != 0 {
			q.page.Limit = p.Limit
		}
		q.page.Offset, q.page.Cursor = p.Offset, p.Cursor
	}
	return Q{q}
}
