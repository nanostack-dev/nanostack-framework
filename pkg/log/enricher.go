package log

import (
	"context"
	"runtime"
	"strings"

	"github.com/rs/zerolog"
)

// Enricher reads one source of ambient state off a context and adds it to the
// logger being built.
//
// It is the seam between this package and an application's own context types.
// The framework cannot read them itself: echopoint keeps identity in a single
// AuthContext struct, anchor in separate scalar context keys, and a future
// service will differ again. Each application registers Enrichers describing
// what it owns, and Bind runs them.
//
// An Enricher is expected to be cheap and total: it runs on every Bind, and a
// context missing its source is normal, not an error. Return c unchanged in
// that case.
type Enricher interface {
	Enrich(ctx context.Context, c zerolog.Context) zerolog.Context
}

// EnricherFunc adapts a function to the Enricher interface.
type EnricherFunc func(ctx context.Context, c zerolog.Context) zerolog.Context

// Enrich implements Enricher.
func (f EnricherFunc) Enrich(ctx context.Context, c zerolog.Context) zerolog.Context {
	return f(ctx, c)
}

// Static returns an Enricher that adds a fixed key/value to every bound logger,
// ignoring the context. Use it for identity a whole subsystem shares, such as
// the component name of a cron.
func Static(key, value string) Enricher {
	return EnricherFunc(func(_ context.Context, c zerolog.Context) zerolog.Context {
		if value == "" {
			return c
		}
		return c.Str(key, value)
	})
}

// callerFrameSkip walks past runtime.Callers itself, callerName, and the
// exported helper that called it (Op), landing on the function the operation
// should be named after.
const callerFrameSkip = 3

// callerName returns the unqualified name of the function two frames above it —
// that is, the caller of the exported helper that called it.
func callerName() string {
	var pcs [1]uintptr
	if runtime.Callers(callerFrameSkip, pcs[:]) == 0 {
		return "unknown"
	}

	frame, _ := runtime.CallersFrames(pcs[:]).Next()
	name := frame.Function
	if name == "" {
		return "unknown"
	}

	// "repo/internal/feature.(*flowService).CreateFlow" -> "CreateFlow"
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	// Method values carry a "-fm" suffix.
	return strings.TrimSuffix(name, "-fm")
}
