package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Format selects the output encoding.
type Format int

const (
	// FormatJSON writes one JSON object per line. It is the zero value because
	// it is the only format a log collector can parse.
	FormatJSON Format = iota
	// FormatText writes key=value lines with a short time and source, for a
	// developer reading a terminal.
	FormatText
)

// LevelOff is above every level a logger emits. A LOG_LEVEL of "off" or
// "disabled" selects it.
const LevelOff = slog.Level(math.MaxInt32)

// Config describes the process handler.
type Config struct {
	// Level is the minimum level to emit. Nil means slog.LevelInfo. Pass a
	// *slog.LevelVar to change the level while the process runs.
	Level slog.Leveler
	// Format is the output encoding. The zero value is FormatJSON.
	Format Format
	// Output receives the encoded lines. Nil means os.Stdout.
	Output io.Writer
	// Service, Environment and Version identify the process on every output
	// line, so a collector can partition several services by name and release.
	// Empty values are omitted.
	Service     string
	Environment string
	Version     string
	// ContextAttrs, when set, returns attributes read from the context of each
	// record, such as the trace ids of its span. They are added to the output
	// lines after the scope and WithAttrs attributes, and not to Export. The
	// tracing package passes tracing.LogAttrs here, which returns trace_id and
	// span_id; core/log itself does not depend on OpenTelemetry.
	ContextAttrs func(context.Context) []slog.Attr
	// Export, when set, receives every record next to the output handler,
	// through slog.NewMultiHandler. The OpenTelemetry module passes
	// otelslog.NewHandler here; that handler reads trace context from the
	// context and the service from its resource, so it gets neither
	// ContextAttrs nor the service identity as attributes.
	Export slog.Handler
}

// ConfigFromEnv reads the configuration from the environment through getenv
// (os.Getenv in production; a map lookup in tests, which keeps them parallel):
//
//   - ENVIRONMENT, default "development".
//   - LOG_LEVEL: debug, info, warn or error, with optional offsets such as
//     "info+2" (see slog.Level.UnmarshalText). The zerolog names trace, fatal,
//     panic and disabled are accepted so existing deployments keep working.
//     Default info.
//   - LOG_FORMAT: json or text. When unset, production and any output that is
//     not a terminal get JSON, and a terminal in development gets text.
//   - SERVICE_NAME and SERVICE_VERSION.
//
// An unknown LOG_LEVEL or LOG_FORMAT is an error, not a silent default: a
// typo would otherwise hide debug output or break log parsing.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Environment: strings.TrimSpace(getenv("ENVIRONMENT")),
		Service:     strings.TrimSpace(getenv("SERVICE_NAME")),
		Version:     strings.TrimSpace(getenv("SERVICE_VERSION")),
		Level:       slog.LevelInfo,
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if raw := strings.TrimSpace(getenv("LOG_LEVEL")); raw != "" {
		level, err := ParseLevel(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.Level = level
	}
	switch raw := strings.ToLower(strings.TrimSpace(getenv("LOG_FORMAT"))); raw {
	case "json":
		cfg.Format = FormatJSON
	case "text":
		cfg.Format = FormatText
	case "":
		if !IsProduction(cfg.Environment) && isTerminal(os.Stdout) {
			cfg.Format = FormatText
		}
	default:
		return Config{}, fmt.Errorf("log: LOG_FORMAT %q: want json or text", raw)
	}
	return cfg, nil
}

// IsProduction reports whether environment names production.
func IsProduction(environment string) bool {
	return strings.EqualFold(environment, "production") || strings.EqualFold(environment, "prod")
}

// ParseLevel parses a slog level name ("debug", "info", "warn", "error", with
// optional offsets) or a zerolog level name. zerolog's trace maps to debug,
// fatal and panic map to error, and disabled maps to LevelOff.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return slog.LevelDebug, nil
	case "warning":
		return slog.LevelWarn, nil
	case "fatal", "panic":
		return slog.LevelError, nil
	case "off", "disabled":
		return LevelOff, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("log: LOG_LEVEL %q: %w", s, err)
	}
	return level, nil
}

func (cfg Config) identity() []slog.Attr {
	var attrs []slog.Attr
	if cfg.Service != "" {
		attrs = append(attrs, slog.String(KeyService, cfg.Service))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, slog.String("env", cfg.Environment))
	}
	if cfg.Version != "" {
		attrs = append(attrs, slog.String("version", cfg.Version))
	}
	return attrs
}

func newOutputHandler(cfg Config) slog.Handler {
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}
	level := cfg.Level
	if level == nil {
		level = slog.LevelInfo
	}
	if cfg.Format == FormatText {
		return slog.NewTextHandler(output, &slog.HandlerOptions{
			AddSource:   true,
			Level:       level,
			ReplaceAttr: compactForTerminal,
		})
	}
	return slog.NewJSONHandler(output, &slog.HandlerOptions{AddSource: true, Level: level})
}

// compactForTerminal shortens the built-in time and source attributes of the
// text format: "15:04:05.000" and "dir/file.go:42". The JSON format keeps the
// standard shape, whose source group also names the function.
func compactForTerminal(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		if t, ok := a.Value.Resolve().Any().(time.Time); ok {
			return slog.String(slog.TimeKey, t.Format("15:04:05.000"))
		}
	case slog.SourceKey:
		if src, ok := a.Value.Resolve().Any().(*slog.Source); ok {
			dir := filepath.Base(filepath.Dir(src.File))
			return slog.String(slog.SourceKey, fmt.Sprintf("%s/%s:%d", dir, filepath.Base(src.File), src.Line))
		}
	}
	return a
}

// isTerminal reports whether f is an interactive character device, as opposed
// to a pipe or a file a container runtime captures.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
