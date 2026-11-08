// Package middleware contributes the request enricher, like echopoint's
// fx.Annotate(requestlog.NewLogEnricher, fx.ResultTags(logging.EnricherGroup)).
package middleware

import (
	"context"
	"log/slog"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/logging"
)

var Module = nano.Module("middleware", nano.Collect[logging.Enricher](NewRequestEnricher))

func NewRequestEnricher() logging.Enricher {
	return func(context.Context) []slog.Attr { return []slog.Attr{slog.String("request_id", "r1")} }
}
