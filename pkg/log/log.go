// Package log provides the context-scoped logger used across Nanostack
// services.
//
// The contract is a single accessor:
//
//	log.Ctx(ctx).Info().Str("flow_id", id).Msg("flow created")
//
// Whatever is ambient — request id, org, user, api key, component — is already
// on the returned logger. Call sites never take a logger dependency, and never
// pass a fallback.
//
// Fields arrive through Bind, which is called once at each entry point (HTTP
// middleware, cron tick, queue job) and runs the application's Enrichers
// against the context. Everything downstream inherits the result, so a
// repository three layers below an HTTP handler logs the caller's org without
// knowing the concept exists.
//
// This package deliberately has no fx dependency; see modules/logging for the
// wiring that builds a Binder from configuration and installs it.
package log

import (
	"context"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// Config describes the process-wide logger identity. Values land on every line
// the service emits, so downstream collectors can partition by service and
// release without parsing the message.
type Config struct {
	// Service names the emitting service, e.g. "echopoint".
	Service string
	// Env is the deployment environment, e.g. "production".
	Env string
	// Version is the build version, typically from buildinfo.
	Version string
	// Level is the minimum severity to emit.
	Level zerolog.Level
}

// Binder builds context-scoped loggers from a base logger and a set of
// Enrichers.
//
// It is safe for concurrent use and holds no mutable state after construction,
// so tests can build one directly instead of reaching for the process default.
type Binder struct {
	base      zerolog.Logger
	enrichers []Enricher
}

// NewBinder returns a Binder that derives loggers from base and applies
// enrichers, in order, on every Bind.
//
// ComponentEnricher is always applied first, so any entry point can name its
// subsystem with WithComponent without the application wiring anything.
func NewBinder(base zerolog.Logger, enrichers ...Enricher) *Binder {
	owned := make([]Enricher, 0, len(enrichers)+1)
	owned = append(owned, ComponentEnricher())
	for _, enricher := range enrichers {
		if enricher != nil {
			owned = append(owned, enricher)
		}
	}
	return &Binder{base: base, enrichers: owned}
}

// Bind returns a context carrying a logger derived from the Binder's base and
// enriched with everything the Enrichers can read from ctx.
//
// Bind always derives from the base logger, never from a logger already on the
// context. That is deliberate, and it imposes one invariant:
//
//	every ambient field must be readable from the context by some Enricher.
//
// Deriving from the context logger instead would let fields accumulate, so
// binding twice would emit duplicate JSON keys — zerolog does not deduplicate.
// Deriving from base makes Bind idempotent, at the cost that a field written
// straight onto a context logger by something other than an Enricher is not
// carried over. requestlog.NewLogEnricher exists for exactly this reason: it
// republishes the request identity that Contextualize established.
//
// Bind is eager: it snapshots ctx at call time. When a value lands afterwards —
// a cron resolving each row's tenant mid-tick — call Bind again once the value
// is in context. Re-binding is cheap and, because of the invariant above,
// lossless.
func (b *Binder) Bind(ctx context.Context) context.Context {
	if b == nil {
		return ctx
	}

	builder := b.base.With()
	for _, enricher := range b.enrichers {
		builder = enricher.Enrich(ctx, builder)
	}
	logger := builder.Logger()

	return logger.WithContext(ctx)
}

// Base returns the Binder's unenriched logger. It is the floor Ctx falls back
// to on a context that was never bound.
func (b *Binder) Base() *zerolog.Logger {
	if b == nil {
		return nil
	}
	return &b.base
}

// defaultBinder is the process-wide Binder installed by modules/logging at
// startup. It is package-level state, which the Nanostack Go guidance otherwise
// avoids: the alternative is threading a Binder into all ~450 call sites that
// only ever read from context, which is the coupling this package exists to
// remove. It is written once during fx construction and only read afterwards.
var defaultBinder atomic.Pointer[Binder] //nolint:gochecknoglobals // documented above; written once at startup.

// Install sets the process-wide Binder used by the package-level Ctx, Op and
// Bind. Call it once, during application construction.
//
// Passing nil clears the installed Binder, which tests can use to restore a
// pristine process. Install returns the previously installed Binder so a test
// can defer its restoration.
func Install(binder *Binder) *Binder {
	return defaultBinder.Swap(binder)
}

// Installed returns the process-wide Binder, or nil when Install has not run.
func Installed() *Binder {
	return defaultBinder.Load()
}

// Bind binds ctx using the installed Binder. On a process where Install has not
// run it returns ctx unchanged, leaving Ctx to fall back to a disabled logger
// rather than panicking.
func Bind(ctx context.Context) context.Context {
	return defaultBinder.Load().Bind(ctx)
}

// Ctx returns the logger bound to ctx.
//
// When ctx carries no logger — a background goroutine nobody bound, a test that
// passes context.Background() — it returns the installed Binder's base logger
// rather than zerolog's disabled one, so a missed Bind degrades to a line
// without ambient fields instead of silence.
func Ctx(ctx context.Context) *zerolog.Logger {
	if logger := zerolog.Ctx(ctx); logger.GetLevel() != zerolog.Disabled {
		return logger
	}
	if base := defaultBinder.Load().Base(); base != nil {
		return base
	}
	return zerolog.Ctx(ctx)
}

// Op returns Ctx(ctx) tagged with the calling function's name as "operation".
//
// The name is read from the caller's stack frame, so it cannot drift from the
// method it describes the way a hand-written string constant does.
//
// Beware closures: called inside a func literal, Op reports the literal
// ("func1"), not the enclosing method. Take the logger before entering the
// closure, or use OpNamed.
func Op(ctx context.Context) *zerolog.Logger {
	return OpNamed(ctx, callerName())
}

// OpNamed returns Ctx(ctx) tagged with an explicit operation name. Use it where
// the caller's frame is not the name you want — inside closures, or when a
// pass has a name of its own.
func OpNamed(ctx context.Context, operation string) *zerolog.Logger {
	logger := Ctx(ctx).With().Str("operation", operation).Logger()
	return &logger
}
