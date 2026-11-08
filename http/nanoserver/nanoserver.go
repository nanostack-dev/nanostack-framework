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
	"sync/atomic"
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
//
// Durations default when zero. Start a Config built by hand, in a test or an
// Override, from DefaultConfig so the port default applies too: a zero Port
// picks a free port.
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
	// stops. Keep it below AppConfig.StopTimeout minus AppConfig.DrainDelay.
	// After it, the remaining connections are closed.
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

	// draining is closed when the drain starts. Draining returns it to the
	// handlers.
	draining chan struct{}
	// open counts the connections that Close would cut: accepted and not
	// yet closed or hijacked.
	open atomic.Int64
}

type drainingKey struct{}

// Draining returns a channel that is closed when the server that serves the
// request starts to drain. A long-lived handler, such as a server-sent events
// stream or a long poll, selects on it to end its response before
// ShutdownTimeout, so the client reconnects to another instance. The channel
// is nil outside a request of a nanoserver Server; a nil channel never fires.
func Draining(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(drainingKey{}).(chan struct{})
	return ch
}

// Defaults of Config. A zero or negative field takes its default, so a zero
// Config is never a server without timeouts.
const (
	DefaultReadHeaderTimeout = 30 * time.Second
	DefaultReadTimeout       = 60 * time.Second
	DefaultWriteTimeout      = 60 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownTimeout   = 5 * time.Second
)

// DefaultConfig returns the Config of the default: tags.
func DefaultConfig() Config {
	return Config{
		Port:              defaultPort,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		ShutdownTimeout:   DefaultShutdownTimeout,
	}
}

const defaultPort = 8080

func (c Config) withDefaults() Config {
	orDefault := func(d *time.Duration, def time.Duration) {
		if *d <= 0 {
			*d = def
		}
	}
	orDefault(&c.ReadHeaderTimeout, DefaultReadHeaderTimeout)
	orDefault(&c.ReadTimeout, DefaultReadTimeout)
	orDefault(&c.WriteTimeout, DefaultWriteTimeout)
	orDefault(&c.IdleTimeout, DefaultIdleTimeout)
	orDefault(&c.ShutdownTimeout, DefaultShutdownTimeout)
	return c
}

// NewServer returns the server for handler. It does not listen yet. A zero
// or negative timeout in cfg takes its default (DefaultReadHeaderTimeout and
// the others).
func NewServer(cfg Config, handler http.Handler, logger *slog.Logger) *Server {
	cfg = cfg.withDefaults()
	s := &Server{
		shutdownTimeout: cfg.ShutdownTimeout,
		logger:          logger.With(slog.String(log.KeyComponent, "httpserver")),
		listening:       make(chan struct{}),
		draining:        make(chan struct{}),
	}
	s.server = &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		BaseContext: func(net.Listener) context.Context {
			return context.WithValue(context.Background(), drainingKey{}, s.draining)
		},
		ConnState: s.track,
	}
	s.server.RegisterOnShutdown(func() { close(s.draining) })
	return s
}

// track keeps open in step with the connections that Close would cut.
func (s *Server) track(_ net.Conn, state http.ConnState) {
	switch state {
	case http.StateNew:
		s.open.Add(1)
	case http.StateHijacked, http.StateClosed:
		s.open.Add(-1)
	case http.StateActive, http.StateIdle:
	}
}

// Run listens and serves until ctx ends, then stops accepting connections,
// closes the Draining channel and waits up to ShutdownTimeout for in-flight
// requests. After ShutdownTimeout it closes the remaining connections and
// logs how many it cut, at Warn: a stream that outlives the drain is
// expected, not a failed shutdown. A listen or serve failure returns at
// once, which shuts the App down: a service that cannot take traffic does
// not keep running. Run returns nil after the drain.
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
		cut := s.open.Load()
		closeErr := s.server.Close()
		if !errors.Is(err, context.DeadlineExceeded) || closeErr != nil {
			return errors.Join(fmt.Errorf("nanoserver: drain: %w", err), closeErr)
		}
		s.logger.WarnContext(drainCtx, "http server drain timed out; connections closed",
			slog.Duration("timeout", s.shutdownTimeout), slog.Int64("connections", cut))
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
