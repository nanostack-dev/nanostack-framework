package log

import (
	"context"

	"github.com/rs/zerolog"
)

type componentContextKey struct{}

// WithComponent marks ctx as belonging to a named subsystem — a cron, a queue
// worker, a background pass — so its log lines can be told apart from request
// traffic and from each other.
//
// Call it at the entry point, before Bind:
//
//	ctx = log.Bind(log.WithComponent(ctx, "flow_schedule_fire"))
//
// It stores the name on the context rather than writing straight to a logger,
// because Bind rebuilds from base and only reads what an Enricher can find on
// the context. ComponentEnricher is registered by default.
func WithComponent(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, componentContextKey{}, name)
}

// ComponentFromContext returns the component name set by WithComponent, or an
// empty string on a context that was never marked — a plain HTTP request.
func ComponentFromContext(ctx context.Context) string {
	if name, ok := ctx.Value(componentContextKey{}).(string); ok {
		return name
	}
	return ""
}

// ComponentEnricher publishes the name set by WithComponent. It is registered
// by NewBinder automatically, so no application has to wire it.
func ComponentEnricher() Enricher {
	return EnricherFunc(func(ctx context.Context, c zerolog.Context) zerolog.Context {
		if name := ComponentFromContext(ctx); name != "" {
			return c.Str("component", name)
		}
		return c
	})
}
