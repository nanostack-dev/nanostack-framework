package logging

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
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
	if strings.EqualFold(config.Environment, "production") {
		logger := zerolog.New(os.Stdout).Level(config.Level).With().Timestamp().Caller().Logger()
		logger.Info().Str("environment", config.Environment).Str("level", logger.GetLevel().String()).Str("format", "json").Msg("zerolog logger created")
		return logger
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).Level(config.Level).With().Timestamp().Caller().Logger()
	logger.Info().Str("environment", config.Environment).Str("level", logger.GetLevel().String()).Str("format", "console").Msg("zerolog logger created")
	return logger
}

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"logging",
	fx.Provide(NewLoggingConfig, NewZerologLogger),
)
