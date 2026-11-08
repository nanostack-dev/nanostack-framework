package nanoserver_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
)

// The generated Build provides the service's handler to NewServer and lets
// the App run it. This example does by hand what the generated code does.
func ExampleNewServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })
	srv := nanoserver.NewServer(nanoserver.Config{Host: "127.0.0.1"}, mux, slog.New(slog.DiscardHandler))

	app := nano.NewApp("billing", nano.AppConfig{})
	app.SetLogger(slog.New(slog.DiscardHandler))
	app.Manage("httpserver", srv)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	<-srv.Listening()

	resp, err := http.Get("http://" + srv.Addr().String() + "/ping")
	if err != nil {
		panic(err)
	}
	_ = resp.Body.Close()
	fmt.Println(resp.StatusCode)
	stop()
	fmt.Println(<-done)
	// Output:
	// 200
	// <nil>
}
