package logging

import (
	"os"
	"strings"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pkg/fxlog"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

type Config struct {
	Environment string
	Level       zerolog.Level
}

func NewLoggingConfig() Config {
	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		environment = "development"
	}
	level := zerolog.InfoLevel
	if rawLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL")); rawLevel != "" {
		if parsedLevel, err := zerolog.ParseLevel(rawLevel); err == nil {
			level = parsedLevel
		}
	}
	return Config{Environment: environment, Level: level}
}

func NewZerologLogger(config Config) zerolog.Logger {
	production := strings.EqualFold(config.Environment, "production") || strings.EqualFold(config.Environment, "prod")
	// The human-friendly console writer is only useful on an interactive
	// terminal (local development). In a container — including the dev server,
	// whose stdout is captured by the Docker json-file driver and shipped to
	// the log collector — emit JSON so structured fields (message, level,
	// caller, ...) survive parsing. ConsoleWriter would otherwise wrap each
	// line in ANSI escapes that the collector cannot parse.
	useConsole := !production && isTerminal(os.Stdout)
	if !useConsole {
		logger := zerolog.New(os.Stdout).Level(config.Level).With().Timestamp().Caller().Logger()
		logger.Info().
			Str("environment", config.Environment).
			Str("level", logger.GetLevel().String()).
			Str("format", "json").
			Msg("zerolog logger created")
		return logger
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		Level(config.Level).
		With().
		Timestamp().
		Caller().
		Logger()
	logger.Info().
		Str("environment", config.Environment).
		Str("level", logger.GetLevel().String()).
		Str("format", "console").
		Msg("zerolog logger created")
	return logger
}

// isTerminal reports whether f is an interactive character device (a TTY), as
// opposed to a pipe or file. Used to pick a human-readable log format only when
// a developer is watching the output live.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"logging",
	fx.Provide(NewLoggingConfig, NewZerologLogger),
)

// WithFxLogger routes Fx's own lifecycle events through the application's
// zerolog logger, so startup/shutdown output is structured JSON instead of Fx's
// default plain-text console writer.
//
// It must be passed at the fx.New root (not nested inside an fx.Module) so it
// governs events from every module. Include it alongside Module:
//
//	fx.New(logging.Module, logging.WithFxLogger(), ...)
func WithFxLogger() fx.Option {
	return fx.WithLogger(func(log zerolog.Logger) fxevent.Logger {
		return fxlog.New(log)
	})
}
