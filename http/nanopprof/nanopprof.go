package nanopprof

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// Module provides *Server, which runs while the App runs.
var Module = nano.Module("pprof", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewServer),
)

// Config is the profiling listener. The defaults are those of the deprecated
// modules/pprof.
type Config struct {
	// Enabled serves the profiles. Off by default: profiles expose memory
	// contents and cost CPU.
	Enabled bool `config:"enabled" default:"false" env:"ENABLE_PPROF" json:"enabled" yaml:"enabled"`
	// Addr is the listen address. Keep it on loopback; reach it through an
	// SSH tunnel or a container exec.
	Addr string `config:"addr" default:"127.0.0.1:6060" json:"addr" yaml:"addr"`
	// ReadHeaderTimeout bounds reading the request headers.
	ReadHeaderTimeout time.Duration `config:"read_header_timeout" default:"30s" json:"read_header_timeout" yaml:"readHeaderTimeout"`
	// ReadTimeout bounds reading the whole request.
	ReadTimeout time.Duration `config:"read_timeout" default:"60s" json:"read_timeout" yaml:"readTimeout"`
	// WriteTimeout bounds writing a profile. A CPU profile of N seconds
	// needs a WriteTimeout above N.
	WriteTimeout time.Duration `config:"write_timeout" default:"60s" json:"write_timeout" yaml:"writeTimeout"`
	// IdleTimeout closes an idle keep-alive connection.
	IdleTimeout time.Duration `config:"idle_timeout" default:"120s" json:"idle_timeout" yaml:"idleTimeout"`
	// ShutdownTimeout bounds the drain when the App stops.
	ShutdownTimeout time.Duration `config:"shutdown_timeout" default:"5s" json:"shutdown_timeout" yaml:"shutdownTimeout"`
}

// Server is the profiling server. It is a nano Runner.
type Server struct {
	enabled         bool
	server          *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger

	listening chan struct{}
	mu        sync.Mutex
	addr      net.Addr
}

// NewServer returns the profiling server. It does not listen yet.
func NewServer(cfg Config, logger *slog.Logger) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return &Server{
		enabled: cfg.Enabled,
		server: &http.Server{
			Addr:              cfg.Addr,
			Handler:           mux,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		},
		shutdownTimeout: cfg.ShutdownTimeout,
		logger:          logger.With(slog.String(log.KeyComponent, "pprof")),
		listening:       make(chan struct{}),
	}
}

// Run serves the profiles until ctx ends, when Enabled. When disabled, it
// serves nothing and only waits for ctx: a Runner that returns early would
// shut the App down. A listen failure returns at once and stops the App, so a
// requested profiler is never silently missing.
func (s *Server) Run(ctx context.Context) error {
	if !s.enabled {
		<-ctx.Done()
		return nil
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("nanopprof: listen on %s: %w", s.server.Addr, err)
	}
	s.mu.Lock()
	s.addr = listener.Addr()
	s.mu.Unlock()
	close(s.listening)
	s.logger.InfoContext(ctx, "pprof server enabled", slog.String("addr", listener.Addr().String()))

	served := make(chan error, 1)
	go func() { served <- s.server.Serve(listener) }()
	select {
	case err = <-served:
		return fmt.Errorf("nanopprof: serve: %w", err)
	case <-ctx.Done():
	}
	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()
	if err = s.server.Shutdown(drainCtx); err != nil {
		return errors.Join(fmt.Errorf("nanopprof: drain: %w", err), s.server.Close())
	}
	return nil
}

// Listening is closed once Run listens. It stays open when disabled.
func (s *Server) Listening() <-chan struct{} { return s.listening }

// Addr returns the bound address, or nil before Run listens.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}
