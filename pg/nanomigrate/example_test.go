package nanomigrate_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

// The generated Build calls Run as a nano.Invoke, with the context of
// nano.Main. With migrations disabled, Run touches nothing.
func ExampleRun() {
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: false}, nil, slog.New(slog.DiscardHandler))
	fmt.Println(nanomigrate.Run(context.Background(), m))
	// Output: <nil>
}
