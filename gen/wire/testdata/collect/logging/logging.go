// Package logging mirrors echopoint's log enrichers: several modules each
// contribute one, and the logger takes them all.
package logging

import (
	"context"
	"log/slog"
)

// Enricher adds request-scoped attributes to a log line.
type Enricher func(ctx context.Context) []slog.Attr

// Enriched applies every Enricher, in declaration order.
type Enriched struct {
	Enrichers []Enricher
}

func NewEnriched(enrichers []Enricher) *Enriched { return &Enriched{Enrichers: enrichers} }

// Keys returns the attribute keys the enrichers add, in order.
func (e *Enriched) Keys(ctx context.Context) []string {
	var keys []string
	for _, enrich := range e.Enrichers {
		for _, a := range enrich(ctx) {
			keys = append(keys, a.Key)
		}
	}
	return keys
}
