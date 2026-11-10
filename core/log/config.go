package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
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

// Wire selects the key names and units of the JSON output.
type Wire int

const (
	// WireSlog writes slog's standard JSON shape: "level":"INFO", "msg", a
	// "source" group and durations in nanoseconds. It is the zero value.
	WireSlog Wire = iota
	// WireLegacy writes the shape of the zerolog lines that the shared log
	// pipeline and its alerts read today: a lowercase "level" (debug, info,
	// warn, error), "message", "caller" as "/abs/path/file.go:64", every
	// duration as float milliseconds and every time as RFC 3339 in whole
	// seconds. It is a bridge: delete it when the collector and the alerts
	// read the slog shape.
	WireLegacy
)

// String returns the LOG_WIRE name of w: "slog" or "legacy".
func (w Wire) String() string {
	if w == WireLegacy {
		return "legacy"
	}
	return "slog"
}

// ParseWire parses a LOG_WIRE value: "slog" or "legacy", in any case. An empty
// value is WireSlog.
func ParseWire(s string) (Wire, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "slog":
		return WireSlog, nil
	case "legacy":
		return WireLegacy, nil
	}
	return WireSlog, fmt.Errorf("log: LOG_WIRE %q: want slog or legacy", s)
}

// ContextAttrs returns attributes read from the context of a record, such as
// the trace ids of its span. tracing.LogAttrs has this shape.
type ContextAttrs func(context.Context) []slog.Attr

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
	// Wire selects the key names and units of FormatJSON. The zero value is
	// WireSlog. FormatText ignores it: a terminal is not parsed.
	Wire Wire
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
	ContextAttrs ContextAttrs
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
//   - LOG_WIRE: slog or legacy (see Wire). Default slog.
//   - SERVICE_NAME and SERVICE_VERSION.
//
// An unknown LOG_LEVEL, LOG_FORMAT or LOG_WIRE is an error, not a silent
// default: a typo would otherwise hide debug output or break log parsing.
//
// It reads the same variables as Settings, which a generated config binder
// fills; both end in Settings.Config.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	return Settings{
		Level:       getenv("LOG_LEVEL"),
		Format:      getenv("LOG_FORMAT"),
		Wire:        getenv("LOG_WIRE"),
		Environment: getenv("ENVIRONMENT"),
		Service:     getenv("SERVICE_NAME"),
		Version:     getenv("SERVICE_VERSION"),
	}.Config()
}

// Settings is the process logging configuration in the form a config binder
// fills: plain strings with config tags. nanostack gen config binds it, and its env
// tags are the variables ConfigFromEnv reads, so a service that moves to typed
// configuration keeps its deployment's names. Call Config to validate it.
//
// The env names are process-global on purpose: one process has one log
// configuration, whatever section the service puts Settings in.
type Settings struct {
	// Level is the minimum level: debug, info, warn or error, with optional
	// offsets such as info+2, or a zerolog name (see ParseLevel).
	Level string `config:"level" env:"LOG_LEVEL" default:"info" json:"level" yaml:"level"`
	// Format is json or text. Empty picks JSON in production and for any
	// output that is not a terminal, and text on a development terminal.
	Format string `config:"format,optional" env:"LOG_FORMAT" json:"format" yaml:"format"`
	// Wire is slog or legacy (see Wire).
	Wire string `config:"wire" env:"LOG_WIRE" default:"slog" json:"wire" yaml:"wire"`
	// Environment names the deployment, such as production. It is written as
	// "env" on every line.
	Environment string `config:"environment" env:"ENVIRONMENT" default:"development" json:"environment" yaml:"environment"`
	// Service names the process on every line.
	Service string `config:"service,optional" env:"SERVICE_NAME" json:"service" yaml:"service"`
	// Version names the running release on every line.
	Version string `config:"version,optional" env:"SERVICE_VERSION" json:"version" yaml:"version"`
}

// Config validates s and returns the Config it describes. Empty Level,
// Environment and Wire take the defaults of the tags (info, development,
// slog), so a Settings built by hand needs no defaults of its own. Output,
// ContextAttrs and Export stay unset for the caller.
func (s Settings) Config() (Config, error) {
	cfg := Config{
		Environment: strings.TrimSpace(s.Environment),
		Service:     strings.TrimSpace(s.Service),
		Version:     strings.TrimSpace(s.Version),
		Level:       slog.LevelInfo,
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if raw := strings.TrimSpace(s.Level); raw != "" {
		level, err := ParseLevel(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.Level = level
	}
	switch raw := strings.ToLower(strings.TrimSpace(s.Format)); raw {
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
	wire, err := ParseWire(s.Wire)
	if err != nil {
		return Config{}, err
	}
	cfg.Wire = wire
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
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
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
	opts := &slog.HandlerOptions{AddSource: true, Level: level}
	if cfg.Wire == WireLegacy {
		opts.ReplaceAttr = legacyWire
	}
	return slog.NewJSONHandler(output, opts)
}

// Legacy wire keys: the names zerolog writes, which the shared collector and
// alerts read.
const (
	legacyMessageKey = "message"
	legacyCallerKey  = "caller"
)

// legacyWire rewrites the attributes of the JSON handler to the zerolog shape
// (see WireLegacy). The level, message and source rules apply at the top level
// only, where slog puts its built-in attributes. The duration and time rules
// apply at any depth, as zerolog's global duration unit and time format do:
// the line's time and every time.Time attribute are RFC 3339 in whole
// seconds, zerolog's default TimeFieldFormat.
func legacyWire(groups []string, a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindDuration:
		return slog.Float64(a.Key, float64(a.Value.Duration())/float64(time.Millisecond))
	case slog.KindTime:
		return slog.String(a.Key, a.Value.Time().Format(time.RFC3339))
	default:
	}
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		if level, ok := a.Value.Any().(slog.Level); ok {
			return slog.String(slog.LevelKey, legacyLevel(level))
		}
	case slog.MessageKey:
		return slog.Attr{Key: legacyMessageKey, Value: a.Value}
	case slog.SourceKey:
		src, ok := a.Value.Any().(*slog.Source)
		if !ok || src == nil || src.File == "" {
			return slog.Attr{} // no caller known: drop the key, as zerolog does
		}
		return slog.String(legacyCallerKey, src.File+":"+strconv.Itoa(src.Line))
	}
	return a
}

// legacyLevel names level as zerolog does, by range: below Info is debug,
// below Warn is info, below Error is warn, and Error and above is error. A
// custom level such as Info+2 thus keeps a name the alerts know.
func legacyLevel(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "debug"
	case level < slog.LevelWarn:
		return "info"
	case level < slog.LevelError:
		return "warn"
	default:
		return "error"
	}
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
