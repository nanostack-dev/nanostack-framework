package log_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

func TestParseWire(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]log.Wire{"": log.WireSlog, "slog": log.WireSlog, "Legacy": log.WireLegacy} {
		got, err := log.ParseWire(in)
		if err != nil || got != want {
			t.Errorf("ParseWire(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := log.ParseWire("zerolog"); err == nil {
		t.Error("ParseWire(zerolog): want error")
	}
	if log.WireLegacy.String() != "legacy" || log.WireSlog.String() != "slog" {
		t.Error("String does not round-trip the LOG_WIRE names")
	}
}

func TestConfigFromEnvReadsWire(t *testing.T) {
	t.Parallel()
	cfg, err := log.ConfigFromEnv(env(map[string]string{"LOG_WIRE": "legacy", "LOG_FORMAT": "json"}))
	if err != nil || cfg.Wire != log.WireLegacy {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}
	if _, err = log.ConfigFromEnv(env(map[string]string{"LOG_WIRE": "zerolog"})); err == nil {
		t.Fatal("unknown LOG_WIRE: want error")
	}
}

func TestSettingsConfig(t *testing.T) {
	t.Parallel()
	cfg, err := log.Settings{Format: "json"}.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "development" || cfg.Level != slog.LevelInfo || cfg.Wire != log.WireSlog {
		t.Fatalf("defaults: cfg = %+v", cfg)
	}
	cfg, err = log.Settings{
		Level: "warn", Format: "text", Wire: "legacy", Environment: "production", Service: "anchor", Version: "2",
	}.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Level != slog.LevelWarn || cfg.Format != log.FormatText || cfg.Wire != log.WireLegacy ||
		cfg.Environment != "production" || cfg.Service != "anchor" || cfg.Version != "2" {
		t.Fatalf("cfg = %+v", cfg)
	}
	for _, s := range []log.Settings{{Level: "loud"}, {Format: "console"}, {Wire: "zerolog"}} {
		if _, configErr := s.Config(); configErr == nil {
			t.Errorf("%+v: want error", s)
		}
	}
}

// TestSettingsTagsMatchConfigFromEnv pins the env names of Settings to the
// variables ConfigFromEnv reads, so a binder and ConfigFromEnv agree.
func TestSettingsTagsMatchConfigFromEnv(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"Level": "LOG_LEVEL", "Format": "LOG_FORMAT", "Wire": "LOG_WIRE",
		"Environment": "ENVIRONMENT", "Service": "SERVICE_NAME", "Version": "SERVICE_VERSION",
	}
	got := map[string]string{}
	for f := range reflect.TypeFor[log.Settings]().Fields() {
		got[f.Name] = f.Tag.Get("env")
	}
	if !maps.Equal(got, want) {
		t.Fatalf("env tags = %v, want %v", got, want)
	}
}

func TestWireLegacyWritesZerologShape(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out, Wire: log.WireLegacy, Level: slog.LevelDebug, Service: "echopoint"})

	logger.WarnContext(t.Context(), "slow call",
		slog.Duration("elapsed", 1500*time.Microsecond),
		slog.Group("db", slog.Duration("wait", 2*time.Second)),
	)
	line := onlyLine(t, out.String())

	if line["level"] != "warn" || line["message"] != "slow call" || line["service"] != "echopoint" {
		t.Fatalf("line = %v", line)
	}
	for _, slogKey := range []string{"msg", "source"} {
		if _, ok := line[slogKey]; ok {
			t.Errorf("legacy line keeps slog key %q: %v", slogKey, line)
		}
	}
	if line["elapsed"] != 1.5 {
		t.Errorf("elapsed = %v, want 1.5 (milliseconds)", line["elapsed"])
	}
	if db, _ := line["db"].(map[string]any); db["wait"] != 2000.0 {
		t.Errorf("db.wait = %v, want 2000 (milliseconds, at any depth)", line["db"])
	}
	caller, _ := line["caller"].(string)
	if !regexp.MustCompile(`(^|/)wire_test\.go:\d+$`).MatchString(filepath.ToSlash(caller)) {
		t.Errorf("caller = %q, want a path ending in wire_test.go:<line>", caller)
	}
}

// TestWireLegacyWritesWholeSecondTimes pins zerolog's default time format:
// RFC 3339 in whole seconds, for the line's time and for every time.Time
// attribute at any depth.
func TestWireLegacyWritesWholeSecondTimes(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out, Wire: log.WireLegacy})
	at := time.Date(2026, 11, 8, 1, 32, 43, 123456789, time.FixedZone("EST", -5*60*60))

	logger.InfoContext(t.Context(), "m", slog.Time("at", at), slog.Group("job", slog.Time("due", at)))
	raw := out.String()
	line := onlyLine(t, raw)

	if line["at"] != "2026-11-08T01:32:43-05:00" {
		t.Errorf("at = %v, want RFC 3339 in whole seconds", line["at"])
	}
	if job, _ := line["job"].(map[string]any); job["due"] != "2026-11-08T01:32:43-05:00" {
		t.Errorf("job.due = %v, want RFC 3339 in whole seconds at any depth", line["job"])
	}
	stamp, _ := line["time"].(string)
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil || strings.Contains(stamp, ".") || parsed.Format(time.RFC3339) != stamp {
		t.Errorf("time = %q, want RFC 3339 in whole seconds (%v)", stamp, err)
	}
}

func TestWireSlogKeepsFractionalTimes(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	at := time.Date(2026, 11, 8, 1, 32, 43, 123456789, time.UTC)
	log.New(log.Config{Output: &out}).InfoContext(t.Context(), "m", slog.Time("at", at))
	if line := onlyLine(t, out.String()); line["at"] != "2026-11-08T01:32:43.123456789Z" {
		t.Fatalf("at = %v", line["at"])
	}
}

func TestWireLegacyLevelNamesByRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug - 4, "debug"}, {slog.LevelDebug, "debug"}, {slog.LevelInfo, "info"},
		{slog.LevelInfo + 2, "info"}, {slog.LevelWarn, "warn"}, {slog.LevelError, "error"},
		{slog.LevelError + 4, "error"},
	}
	for _, tt := range tests {
		var out syncBuffer
		logger := log.New(log.Config{Output: &out, Wire: log.WireLegacy, Level: slog.LevelDebug - 4})
		logger.Log(t.Context(), tt.level, "m")
		if got := onlyLine(t, out.String())["level"]; got != tt.want {
			t.Errorf("level %v = %v, want %q", tt.level, got, tt.want)
		}
	}
}

func TestWireSlogIsUnchanged(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	log.New(log.Config{Output: &out}).InfoContext(t.Context(), "m", slog.Duration("d", time.Millisecond))
	line := onlyLine(t, out.String())
	if line["level"] != "INFO" || line["msg"] != "m" || line["d"] != float64(time.Millisecond) ||
		line["source"] == nil {
		t.Fatalf("line = %v", line)
	}
}

func TestWireDoesNotChangeTextFormat(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	log.New(log.Config{Output: &out, Format: log.FormatText, Wire: log.WireLegacy}).InfoContext(t.Context(), "m")
	if !strings.Contains(out.String(), "level=INFO") || !strings.Contains(out.String(), "msg=m") {
		t.Fatalf("text line = %q", out.String())
	}
}

// zerologSummary is the summary line of pkg/httputil/requestlog (zerolog, as
// echopoint and anchor run it with modules/logging: Timestamp and Caller), for
// GET /flows/123?expand=steps&page=2. It was written by requestlog.New behind
// requestlog.Contextualize at v0.22.0 (requestlog.go:129-163), with the caller
// path shortened. core/log may not import zerolog, so the line is a fixture.
const zerologSummary = `{"level":"info","request_id":"req-1","method":"GET","path":"/flows/123",` +
	`"status":200,"bytes_out":5,"duration":2.315292,"client_ip":"203.0.113.7",` +
	`"query":"expand=steps&page=2","user_agent":"test","time":"2026-11-08T01:32:43-05:00",` +
	`"caller":"/src/pkg/httputil/requestlog/requestlog.go:162","message":"GET /flows/123 -> 200 (2.315ms)"}`

func legacySummary(t *testing.T, ctx context.Context, target string) map[string]any {
	t.Helper()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out, Wire: log.WireLegacy})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /flows/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger, LegacySummary: true})(mux)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	req.Header.Set("User-Agent", "test")
	req.Header.Set(log.RequestIDHeader, "req-1")
	req.RemoteAddr = "203.0.113.7:51234"
	h.ServeHTTP(httptest.NewRecorder(), req)
	return onlyLine(t, out.String())
}

// TestLegacySummaryMatchesZerologRequestLog is the golden test of the bridge:
// with WireLegacy and LegacySummary, the summary line has the keys, level,
// query and duration unit of the zerolog line the alerts read.
func TestLegacySummaryMatchesZerologRequestLog(t *testing.T) {
	t.Parallel()
	var zl map[string]any
	if err := json.Unmarshal([]byte(zerologSummary), &zl); err != nil {
		t.Fatal(err)
	}
	line := legacySummary(t, t.Context(), "/flows/123?expand=steps&page=2")

	// Additive: route, the ServeMux pattern.
	got := slices.Sorted(maps.Keys(line))
	want := slices.Sorted(slices.Values(append(slices.Collect(maps.Keys(zl)), log.KeyRoute)))
	if !slices.Equal(got, want) {
		t.Fatalf("keys = %v\nwant %v", got, want)
	}
	for _, k := range []string{"level", "request_id", "method", "path", "status", "bytes_out", "client_ip", "query", "user_agent"} {
		if line[k] != zl[k] {
			t.Errorf("%s = %v, zerolog %v", k, line[k], zl[k])
		}
	}
	d, ok := line[log.KeyDuration].(float64)
	if !ok || d <= 0 || d > float64(time.Minute/time.Millisecond) {
		t.Fatalf("duration = %v, want float milliseconds", line[log.KeyDuration])
	}
	// The message prints the duration as time.Duration does, rounded to the
	// microsecond.
	message, _ := line["message"].(string)
	if !regexp.MustCompile(`^GET /flows/123 -> 200 \([0-9]+(\.[0-9]+)?(µs|ms|s)\)$`).MatchString(message) {
		t.Errorf("message = %q, zerolog %q", message, zl["message"])
	}
	stamp, _ := line["time"].(string)
	if _, err := time.Parse(time.RFC3339, stamp); err != nil || strings.Contains(stamp, ".") {
		t.Errorf("time = %q, want RFC 3339 in whole seconds like %v", stamp, zl["time"])
	}
	caller, _ := line["caller"].(string)
	if !regexp.MustCompile(`(^|/)core/log/http\.go:\d+$`).MatchString(filepath.ToSlash(caller)) {
		t.Errorf("caller = %q, want a path ending in core/log/http.go:<line>", caller)
	}
}

func TestLegacySummaryLevelFollowsZerologRule(t *testing.T) {
	t.Parallel()
	for status, want := range map[int]string{
		http.StatusNotFound: "info", http.StatusInternalServerError: "error", 599: "error", 600: "info",
	} {
		var out syncBuffer
		logger := log.New(log.Config{Output: &out, Wire: log.WireLegacy})
		h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger, LegacySummary: true})(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/runs", nil))
		line := onlyLine(t, out.String())
		message, _ := line["message"].(string)
		if line["level"] != want || !strings.HasPrefix(message, "POST /runs -> "+strconv.Itoa(status)+" (") {
			t.Errorf("status %d: line = %v, want level %s", status, line, want)
		}
	}
}

func TestSlogSummaryKeepsConstantMessageWithoutSource(t *testing.T) {
	t.Parallel()
	var out syncBuffer
	logger := log.New(log.Config{Output: &out})
	h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(600) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	line := onlyLine(t, out.String())
	if line["msg"] != "http request" || line["source"] != nil || line["level"] != "ERROR" {
		t.Fatalf("line = %v", line)
	}
}

func TestLegacySummaryLogsCancelledRequestAtItsStatusLevel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	line := legacySummary(t, ctx, "/flows/123")
	if line["level"] != "info" || line[log.KeyClientCanceled] != true {
		t.Fatalf("line = %v, want level info with client_canceled", line)
	}
}

func TestLegacySummaryRedactsQueryString(t *testing.T) {
	t.Parallel()
	line := legacySummary(t, t.Context(), "/flows/123?b=2&access_token=abc&a=x%20y&bad%zz=1&&api_key=k&x=1;token=t")
	want := "b=2&access_token=" + secret.Redacted + "&a=x%20y&api_key=" + secret.Redacted
	if line[log.KeyQuery] != want {
		t.Fatalf("query = %q, want %q", line[log.KeyQuery], want)
	}
}

// TestSummaryKeys pins the summary keys: the collector renames method and
// path, and the alerts read status and duration.
func TestSummaryKeys(t *testing.T) {
	t.Parallel()
	got := []string{
		log.KeyMethod, log.KeyPath, log.KeyRoute, log.KeyStatus, log.KeyBytesOut, log.KeyDuration,
		log.KeyClientIP, log.KeyQuery, log.KeyUserAgent, log.KeyClientCanceled, log.KeyHijacked, log.KeyPanic,
	}
	want := []string{
		"method", "path", "route", "status", "bytes_out", "duration",
		"client_ip", "query", "user_agent", "client_canceled", "hijacked", "panic",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
}
