// Package httpapi serves the greeter over HTTP, with the App's probes.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/greeter"
)

type Config struct {
	Addr string
}

// Server is a Starter (it binds the port, so a busy port fails start) and a
// Runner (it serves until the App cancels it).
type Server struct {
	cfg Config
	log *slog.Logger
	srv *http.Server
	ln  net.Listener
}

func NewServer(cfg Config, log *slog.Logger, svc *greeter.Service, app *nano.App) *Server {
	mux := http.NewServeMux()
	mux.Handle("GET /livez", app.AdminHandler())
	mux.Handle("GET /readyz", app.AdminHandler())
	mux.HandleFunc("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
		msg, err := svc.Greet(r.PathValue("name"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(msg + "\n"))
	})
	return &Server{cfg: cfg, log: log, srv: &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}}
}

func (s *Server) Start(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.log.Info("listening", "addr", ln.Addr().String())
	return nil
}

func (s *Server) Run(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() { errc <- s.srv.Serve(s.ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
