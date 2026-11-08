package log

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
)

// Err returns the attribute for err under KeyError. Use it rather than an
// ad-hoc key, so every error is found under one key.
func Err(err error) slog.Attr {
	return slog.Any(KeyError, err)
}

// LevelFor returns the level that fits err. It is the one place that maps an
// error to a level; ErrorStatusTo applies the same rules with a status the
// caller already chose.
//
// "An error happened" and "the service is broken" are different claims, and
// only the second should page someone:
//
//   - Cancellation and deadline expiry are Warn: the caller went away, the
//     service did not fail. They are checked first, because a cancelled
//     request often also carries a wrapper from the layer that gave up.
//   - An error that implements slog.Leveler (a Level() slog.Level method)
//     anywhere in its chain picks its own level. A package whose errors are
//     the caller's fault, such as an invalid search cursor, uses this.
//   - An error with a 4xx status is Debug: a 404 or a validation failure is
//     the API answering correctly. The status comes from the ProblemStatus()
//     method of core/fault's Described interface, which every generated
//     error implements, anywhere in the chain. A Described for which
//     fault.Valid is false (a status outside 400 to 599, or an empty type)
//     counts as none, as the problem writer renders such an error as a 500.
//   - Everything else is Error.
func LevelFor(err error) slog.Level {
	return levelFor(err, httpStatus(err))
}

// levelFor applies the rules of LevelFor with status as the error's HTTP
// status; 0 means none.
func levelFor(err error, status int) slog.Level {
	if err == nil {
		return slog.LevelError
	}
	if IsContextError(err) {
		return slog.LevelWarn
	}
	if leveler, ok := errors.AsType[levelError](err); ok {
		return leveler.Level()
	}
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		return slog.LevelDebug
	}
	return slog.LevelError
}

// IsContextError reports whether err is a cancellation or a deadline expiry.
// Callers use it for control flow too: work abandoned because the caller left
// is not a failure to retry or report.
//
// A driver that reports a cancelled statement without wrapping
// context.Canceled (lib/pq's SQLSTATE 57014) must wrap it at the data layer
// for this to match; this package does not import database drivers.
func IsContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// levelError is an error that picks its own log level.
type levelError interface {
	error
	Level() slog.Level
}

// statusError is an error that maps to an HTTP status through the status
// method of core/fault's Described interface. It is declared structurally so
// that the legacy pkg/fault errors, which have ProblemStatus but no
// ProblemType, match too.
type statusError interface {
	error
	ProblemStatus() int
}

// httpStatus returns the HTTP error status err carries, anywhere in its chain,
// or 0 when there is none. A fault.Described counts only when fault.Valid
// holds, the rule the problem writer applies before it renders the status. A
// legacy error with only ProblemStatus counts when fault.IsErrorStatus holds.
func httpStatus(err error) int {
	s, ok := errors.AsType[statusError](err)
	if !ok {
		return 0
	}
	if d, described := s.(fault.Described); described && !fault.Valid(d) {
		return 0
	}
	if status := s.ProblemStatus(); fault.IsErrorStatus(status) {
		return status
	}
	return 0
}

// Error logs msg with err on the default logger, at the level LevelFor picks.
// The line carries err under KeyError and, when err has an HTTP status, that
// status under "status". args are extra attributes, as for slog.Log.
//
//	log.Error(ctx, err, "load flow", "flow_id", id)
//
// The source location is the caller of Error.
func Error(ctx context.Context, err error, msg string, args ...any) {
	logError(ctx, slog.Default(), err, httpStatus(err), msg, args)
}

// ErrorTo is Error on logger. A nil logger means slog.Default().
func ErrorTo(ctx context.Context, logger *slog.Logger, err error, msg string, args ...any) {
	if logger == nil {
		logger = slog.Default()
	}
	logError(ctx, logger, err, httpStatus(err), msg, args)
}

// ErrorStatusTo is ErrorTo for an error whose HTTP response status the caller
// already chose, such as the problem writer, which also answers errors that
// carry no status (a 500, or a 499 for a cancelled request). The level follows
// the rules of LevelFor with that status, and the line carries it under
// "status". A nil logger means slog.Default().
func ErrorStatusTo(ctx context.Context, logger *slog.Logger, err error, status int, msg string, args ...any) {
	if logger == nil {
		logger = slog.Default()
	}
	logError(ctx, logger, err, status, msg, args)
}

// errorCallerSkip skips runtime.Callers, logError and the exported Error,
// ErrorTo or ErrorStatusTo, so the record's source is their caller.
const errorCallerSkip = 3

// logError writes the record; status 0 means none.
func logError(ctx context.Context, logger *slog.Logger, err error, status int, msg string, args []any) {
	level := levelFor(err, status)
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(errorCallerSkip, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	if err != nil {
		r.AddAttrs(Err(err))
	}
	if status != 0 {
		r.AddAttrs(slog.Int(KeyStatus, status))
	}
	r.Add(args...)
	_ = logger.Handler().Handle(ctx, r) // like slog.Logger, a failed write is not the caller's error
}
