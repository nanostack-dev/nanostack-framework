// Package example is a small service built with nanostack gen wire. Its manifest
// is nano.go; cmd/example runs it.
package example

import (
	"context"
	"log/slog"
	"os"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/greeter"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/httpapi"
)

// Config is the service configuration. Build provides each field to the
// providers that take its type.
type Config struct {
	App      nano.AppConfig
	HTTP     httpapi.Config
	Greeting greeter.Config
}

// NewLogger writes text logs to stderr. Build hands it to the App too.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

// Seed stores the names the greeter knows at boot.
func Seed(ctx context.Context, s greeter.Store) error {
	for _, name := range []string{"ada", "grace"} {
		if err := s.Put(ctx, name); err != nil {
			return err
		}
	}
	return nil
}
