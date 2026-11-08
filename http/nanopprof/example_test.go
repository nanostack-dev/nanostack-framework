package nanopprof_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nanostack-dev/nanostack-framework/http/nanopprof"
)

// A disabled server serves nothing; its Run only waits for the App to stop.
func ExampleNewServer() {
	srv := nanopprof.NewServer(nanopprof.Config{Enabled: false}, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fmt.Println(srv.Run(ctx), srv.Addr())
	// Output: <nil> <nil>
}
