package logging_test

import (
	"context"
	"log/slog"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/logging"
)

// A service lists logging.Module in its manifest and log.Settings in its
// Config. The generated Build calls NewLogger with the bound settings and
// every log.ContextAttrs member; this example calls it by hand.
func ExampleNewLogger() {
	previous := slog.Default()
	defer slog.SetDefault(previous)

	requestTenant := func(context.Context) []slog.Attr { return nil }
	logger, err := logging.NewLogger(
		log.Settings{Level: "warn", Format: "json", Wire: "legacy", Service: "billing"},
		[]log.ContextAttrs{requestTenant},
	)
	if err != nil {
		panic(err)
	}
	logger.InfoContext(context.Background(), "below warn, not written")
	// Output:
}
