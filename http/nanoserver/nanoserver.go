package nanoserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// Module provides *Server, which runs while the App runs.
var Module = nano.Module("httpserver", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewServer),
)

// Config is the listener and its timeouts. The defaults are those of the
// deprecated modules/httpserver.
type Config struct {
	// Host is the address to bind. Empty binds every interface.
	Host string `config:"host,optional" json:"host" yaml:"host"`
	// Port is the TCP port. 0 picks a free port, for tests.
	Port int `config:"port" default:"8080" validate:"min=0,max=65535" json:"port" yaml:"port"`
	// ReadHeaderTimeout bounds reading the request headers.
	ReadHeaderTimeout time.Duration `config:"read_header_timeout" default:"30s" json:"read_header_timeout" yaml:"readHeaderTimeout"`
	// ReadTimeout bounds reading the whole request.
	ReadTimeout time.Duration `config:"read_timeout" default:"60s" json:"read_timeout" yaml:"readTimeout"`
	// WriteTimeout bounds writing the response. Streaming routes extend it
	// per request with http.ResponseController.
	WriteTimeout time.Duration `config:"write_timeout" default:"60s" json:"write_timeout" yaml:"writeTimeout"`
	// IdleTimeout closes an idle keep-alive connection.
	IdleTimeout time.Duration `config:"idle_timeout" default:"120s" json:"idle_timeout" yaml:"idleTimeout"`
	// ShutdownTimeout bounds the drain of in-flight requests when the App
	// stops. Keep it below AppConfig.StopTimeout. After it, the remaining
	// connections are closed.
	ShutdownTimeout time.Duration `config:"shutdown_timeout" default:"5s" json:"shutdown_timeout" yaml:"shutdownTimeout"`
}

// Server is the HTTP server. It is a nano Runner.
type Server struct {
	server          *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger

	listening chan struct{}
	mu        sync.Mutex
	addr      net.Addr
}

// NewServer returns the server for handler. It does not listen yet.
func NewServer(cfg Config, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{
		server: &http.Server{
			Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		},
		shutdownTimeout: cfg.ShutdownTimeout,
		logger:          logger.With(slog.String(log.KeyComponent, "httpserver")),
		listening:       make(chan struct{}),
	}
}

// Run listens and serves until ctx ends, then stops accepting connections
// and waits up to ShutdownTimeout for in-flight requests. A listen or serve
// failure returns at once, which shuts the App down: a service that cannot
// take traffic does not keep running. Run returns nil after a clean drain.
func (s *Server) Run(ctx context.Context) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("nanoserver: listen on %s: %w", s.server.Addr, err)
	}
	s.mu.Lock()
	s.addr = listener.Addr()
	s.mu.Unlock()
	close(s.listening)
	s.logger.InfoContext(ctx, "http server started", slog.String("addr", listener.Addr().String()))

	served := make(chan error, 1)
	go func() { served <- s.server.Serve(listener) }()
	select {
	case err = <-served:
		return fmt.Errorf("nanoserver: serve: %w", err)
	case <-ctx.Done():
	}

	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()
	s.logger.InfoContext(drainCtx, "http server draining", slog.Duration("timeout", s.shutdownTimeout))
	if err = s.server.Shutdown(drainCtx); err != nil {
		closeErr := s.server.Close()
		return errors.Join(fmt.Errorf("nanoserver: drain: %w", err), closeErr)
	}
	if err = <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("nanoserver: serve: %w", err)
	}
	return nil
}

// Listening is closed once Run listens. Then Addr returns the bound address.
func (s *Server) Listening() <-chan struct{} { return s.listening }

// Addr returns the bound address, or nil before Run listens.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}
