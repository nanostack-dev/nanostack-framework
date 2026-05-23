package pprof

import (
	"context"
	"errors"
	"net/http"
	stdpprof "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

const (
	defaultAddr            = "127.0.0.1:6060"
	defaultEnv             = "ENABLE_PPROF"
	defaultShutdownTimeout = 5 * time.Second
)

type Options struct {
	Addr              string
	EnableEnv         string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

func (o Options) withDefaults() Options {
	if o.Addr == "" {
		o.Addr = defaultAddr
	}
	if o.EnableEnv == "" {
		o.EnableEnv = defaultEnv
	}
	if o.ReadHeaderTimeout == 0 {
		o.ReadHeaderTimeout = 30 * time.Second
	}
	if o.ReadTimeout == 0 {
		o.ReadTimeout = 60 * time.Second
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 60 * time.Second
	}
	if o.IdleTimeout == 0 {
		o.IdleTimeout = 120 * time.Second
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = defaultShutdownTimeout
	}
	return o
}

type Params struct {
	fx.In
	Lifecycle fx.Lifecycle
	Logger    zerolog.Logger
}

func NewModule(options Options) fx.Option {
	options = options.withDefaults()
	return fx.Module("pprof", fx.Invoke(func(params Params) { Register(params, options) }))
}

func Register(params Params, options Options) {
	options = options.withDefaults()
	var server *http.Server
	params.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if !isEnabled(options.EnableEnv) {
				return nil
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", stdpprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", stdpprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", stdpprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", stdpprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", stdpprof.Trace)
			server = &http.Server{
				Addr:              options.Addr,
				Handler:           mux,
				ReadHeaderTimeout: options.ReadHeaderTimeout,
				ReadTimeout:       options.ReadTimeout,
				WriteTimeout:      options.WriteTimeout,
				IdleTimeout:       options.IdleTimeout,
			}
			go func() {
				params.Logger.Info().Str("addr", options.Addr).Str("env", options.EnableEnv).Msg("pprof server enabled")
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					params.Logger.Error().Err(err).Msg("pprof server stopped")
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if server == nil {
				return nil
			}
			shutdownCtx, cancel := context.WithTimeout(ctx, options.ShutdownTimeout)
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
	})
}

func isEnabled(env string) bool {
	value := strings.TrimSpace(os.Getenv(env))
	return strings.EqualFold(value, "true") || value == "1"
}
