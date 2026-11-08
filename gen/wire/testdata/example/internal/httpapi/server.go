// Package httpapi serves the greeter over HTTP, and the App's probes on a
// separate admin listener: /readyz is unauthenticated, so it stays off the
// API's port.
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
	// Addr is the API's listen address.
	Addr string
	// AdminAddr is the listen address of /livez and /readyz. Keep it off the
	// public network.
	AdminAddr string
}

// Server is a Starter (it binds the ports, so a busy port fails start) and a
// Runner (it serves until the App cancels it).
type Server struct {
	cfg      Config
	log      *slog.Logger
	srv      *http.Server
	admin    *http.Server
	ln, adln net.Listener
}

func NewServer(cfg Config, log *slog.Logger, svc *greeter.Service, app *nano.App) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
		msg, err := svc.Greet(r.PathValue("name"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(msg + "\n"))
	})
	return &Server{
		cfg:   cfg,
		log:   log,
		srv:   &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second},
		admin: &http.Server{Handler: app.AdminHandler(), ReadHeaderTimeout: 5 * time.Second},
	}
}

func (s *Server) Start(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	adln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.AdminAddr)
	if err != nil {
		_ = ln.Close()
		return err
	}
	s.ln, s.adln = ln, adln
	s.log.Info("listening", "addr", ln.Addr().String(), "admin_addr", adln.Addr().String())
	return nil
}

func (s *Server) Run(ctx context.Context) error {
	errc := make(chan error, 2)
	go func() { errc <- s.srv.Serve(s.ln) }()
	go func() { errc <- s.admin.Serve(s.adln) }()
	received := 0
	var err error
	select {
	case err = <-errc: // a listener failed
		received++
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = errors.Join(err, s.srv.Shutdown(shutdownCtx), s.admin.Shutdown(shutdownCtx))
	for ; received < cap(errc); received++ {
		if serveErr := <-errc; !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
	}
	return err
}
