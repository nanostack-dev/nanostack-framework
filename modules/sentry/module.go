package sentry

import (
	"context"
	"net/http"
	"strings"
	"time"

	getsentry "github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/nanostack-dev/nanostack-framework/modules/config"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

const flushTimeout = 2 * time.Second

type Config struct {
	DSN              string  `yaml:"dsn"                optional:"true"`
	Environment      string  `yaml:"environment"        optional:"true"`
	Release          string  `yaml:"release"            optional:"true"`
	EnableTracing    bool    `yaml:"enable_tracing"     optional:"true"`
	TracesSampleRate float64 `yaml:"traces_sample_rate" optional:"true"`
}

var Module = fx.Module("sentry", fx.Invoke(initialize)) //nolint:gochecknoglobals // Required for fx module definition.

func NewModule() fx.Option { return Module }

func initialize(lifecycle fx.Lifecycle, loader config.Loader, logger zerolog.Logger) {
	var cfg Config
	if err := loader.LoadConfig("sentry", &cfg); err != nil {
		logger.Info().Msg("sentry configuration not found; issues disabled")
		return
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		logger.Info().Msg("sentry dsn empty; issues disabled")
		return
	}
	opts := getsentry.ClientOptions{Dsn: cfg.DSN, Environment: cfg.Environment, Release: cfg.Release, AttachStacktrace: true}
	if cfg.EnableTracing || cfg.TracesSampleRate > 0 {
		opts.EnableTracing = true
		opts.TracesSampleRate = cfg.TracesSampleRate
		if opts.TracesSampleRate <= 0 {
			opts.TracesSampleRate = 1.0
		}
	}
	if err := getsentry.Init(opts); err != nil {
		logger.Error().Err(err).Msg("failed to initialize sentry")
		return
	}
	lifecycle.Append(fx.Hook{OnStop: func(context.Context) error {
		getsentry.Flush(flushTimeout)
		return nil
	}})
}

func Enabled() bool { return getsentry.CurrentHub().Client() != nil }

func CaptureException(err error) {
	if err != nil && Enabled() {
		getsentry.CaptureException(err)
	}
}

func HTTPMiddleware() func(http.Handler) http.Handler {
	if !Enabled() {
		return func(next http.Handler) http.Handler { return next }
	}
	return sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle
}
