package log_test

import (
	"log/slog"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestConfigFromEnv(t *testing.T) {
	t.Parallel()
	cfg, err := log.ConfigFromEnv(env(map[string]string{
		"ENVIRONMENT": "production", "LOG_LEVEL": "debug", "SERVICE_NAME": "echopoint", "SERVICE_VERSION": "1.0.0",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "production" || cfg.Level != slog.LevelDebug || cfg.Format != log.FormatJSON ||
		cfg.Service != "echopoint" || cfg.Version != "1.0.0" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestConfigFromEnvDefaultsAndOverrides(t *testing.T) {
	t.Parallel()
	cfg, err := log.ConfigFromEnv(env(map[string]string{"LOG_FORMAT": "text"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "development" || cfg.Level != slog.LevelInfo || cfg.Format != log.FormatText {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestConfigFromEnvRejectsTypos(t *testing.T) {
	t.Parallel()
	for _, values := range []map[string]string{{"LOG_LEVEL": "verbose"}, {"LOG_FORMAT": "console"}} {
		if _, err := log.ConfigFromEnv(env(values)); err == nil {
			t.Errorf("%v: want error", values)
		}
	}
}

func TestParseLevel(t *testing.T) {
	t.Parallel()
	tests := map[string]slog.Level{
		"debug": slog.LevelDebug, "INFO": slog.LevelInfo, "warn": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, "info+2": slog.LevelInfo + 2,
		"trace": slog.LevelDebug, "fatal": slog.LevelError, "panic": slog.LevelError, "disabled": log.LevelOff,
	}
	for in, want := range tests {
		got, err := log.ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}
