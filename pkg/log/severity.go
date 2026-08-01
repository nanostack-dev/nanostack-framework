package log

import (
	"context"
	"errors"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/rs/zerolog"
)

// LevelFor returns the severity that fits err.
//
// The rule exists because "an error happened" and "something is wrong with the
// service" are different claims, and only the second should reach an alert:
//
//   - Context cancellation and deadline expiry are Warn. They happen routinely
//     when a client disconnects or an upstream gives up; the caller went away,
//     the service did not fail.
//   - A handled API error below 500 is Debug. A 404 or a validation failure is
//     the API answering correctly, and paging on it trains people to ignore the
//     pager.
//   - Everything else is Error.
//
// Cancellation is checked first: a cancelled request often also carries a fault
// wrapper from the layer that gave up, and the cancellation is the truer story.
func LevelFor(err error) zerolog.Level {
	if err == nil {
		return zerolog.ErrorLevel
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return zerolog.WarnLevel
	}
	if apiErr, ok := fault.As(err); ok && apiErr != nil {
		if apiErr.HTTPStatus() < http.StatusInternalServerError {
			return zerolog.DebugLevel
		}
	}
	return zerolog.ErrorLevel
}

// Event returns an event for err at the severity LevelFor picks, with err and,
// when the error carries one, its HTTP status already attached.
//
// The caller supplies only the message:
//
//	log.Event(log.Ctx(ctx), err).Msg("failed to load flow")
//
// Do not add .Err(err) at the call site — it is already attached, and repeating
// it emits a duplicate "error" key.
func Event(logger *zerolog.Logger, err error) *zerolog.Event {
	if logger == nil {
		nop := zerolog.Nop()
		return nop.Error().Err(err)
	}

	event := logger.WithLevel(LevelFor(err)).Err(err)
	if apiErr, ok := fault.As(err); ok && apiErr != nil {
		event = event.Int("status", apiErr.HTTPStatus())
	}
	return event
}

// Error is Event against the logger bound to ctx.
func Error(ctx context.Context, err error) *zerolog.Event {
	return Event(Ctx(ctx), err)
}
