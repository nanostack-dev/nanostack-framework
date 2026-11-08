package nanotrace_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/otel/nanotrace"
)

// The generated Build calls NewProvider with the bound Config and the
// process log.Settings; the App starts the Installer and stops the Provider.
// Without an OTLP endpoint the provider records nothing and exports nothing.
func ExampleNewProvider() {
	ctx := context.Background()
	provider, err := nanotrace.NewProvider(ctx, nanotrace.DefaultConfig(),
		log.Settings{Service: "billing"}, slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	defer func() { _ = provider.Stop(ctx) }()

	fmt.Println(provider.Exporting())
	// Output: false
}
