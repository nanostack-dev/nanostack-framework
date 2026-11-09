package search

import (
	"errors"
	"log/slog"
)

// The client errors of a search. Each answers HTTP 400, and each has a Level
// method that returns slog.LevelDebug, so core/log's LevelFor logs it at Debug
// through any wrapping: the API refusing a bad request is not the service
// failing.
var (
	// ErrInvalidCursor reports a cursor that cannot continue this search: it
	// is malformed, too long, or was issued for a different ordering.
	ErrInvalidCursor error = &clientError{"search: invalid cursor"}

	// ErrCursorWithOffset reports a page that sets both a cursor and a
	// non-zero offset. A cursor already says where the page starts.
	ErrCursorWithOffset error = &clientError{"search: cursor cannot be combined with offset"}

	// ErrUnknownSort reports a sort field the search cannot order by. A
	// silent fallback to the default order would answer another question
	// than the client asked.
	ErrUnknownSort error = &clientError{"search: unknown sort field"}

	// ErrInvalidLimit reports a page size outside 1 to MaxLimit.
	ErrInvalidLimit error = &clientError{"search: limit out of range"}

	// ErrInvalidOffset reports a negative offset.
	ErrInvalidOffset error = &clientError{"search: offset out of range"}

	// ErrTooManyValues reports a set filter with more than MaxInValues values.
	ErrTooManyValues error = &clientError{"search: too many values"}

	// ErrTermTooLong reports a text term longer than MaxTermLength characters.
	ErrTermTooLong error = &clientError{"search: term too long"}
)

// ErrUnscoped reports a search that has no tenant scope: it was not built from
// its generated spec, a scope value is zero (an empty id, a nil UUID), or a
// context scope returned no condition. It is a programming error, not a client
// error: it has no Level method, so core/log logs it at Error, and the HTTP
// edge answers it with a 500.
var ErrUnscoped = errors.New("search: no tenant scope; build the search from its generated spec")

// clientError is a search error the caller caused. It carries no HTTP status:
// the transport maps each one to its own 400 code.
type clientError struct{ msg string }

func (e *clientError) Error() string { return e.msg }

// Level returns slog.LevelDebug, the level of a request the API refuses.
func (*clientError) Level() slog.Level { return slog.LevelDebug }

// IsClientError reports whether err is a search error the caller caused, at
// any depth of wrapping. Each answers HTTP 400. ErrUnscoped is not one, and it
// wins: an error that also holds ErrUnscoped (for example through errors.Join)
// is not a client error, because a missing tenant scope is a programming error
// even when the request was also bad.
//
// core/log's LevelFor reads the first Level method it finds, and core cannot
// import db. So a join of a client error and ErrUnscoped can still log at
// Debug there. Wrap ErrUnscoped alone, never in a join with a client error.
func IsClientError(err error) bool {
	if errors.Is(err, ErrUnscoped) {
		return false
	}
	_, ok := errors.AsType[*clientError](err)
	return ok
}
