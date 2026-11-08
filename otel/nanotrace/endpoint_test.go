package nanotrace

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

// collector records the path of every OTLP request.
func collector(t *testing.T) (base string, paths func() []string) {
	t.Helper()
	var (
		mu  sync.Mutex
		got []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// TestConfiguredEndpointReachesTheExporter covers a Config bound from a YAML
// layer: the exporter must send to its endpoint, not to the OTEL_* variables
// of the process nor to the SDK default.
func TestConfiguredEndpointReachesTheExporter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://127.0.0.1:1/v1/traces")
	for name, tc := range map[string]struct {
		cfg  func(base string) Config
		want string
	}{
		"base endpoint": {func(b string) Config { return Config{Endpoint: b} }, "/v1/traces"},
		"base endpoint with a path": {
			func(b string) Config { return Config{Endpoint: b + "/otlp/"} }, "/otlp/v1/traces",
		},
		"traces endpoint as given": {
			func(b string) Config { return Config{TracesEndpoint: b + "/custom"} }, "/custom",
		},
		"traces endpoint wins": {
			func(b string) Config { return Config{Endpoint: b + "/base", TracesEndpoint: b + "/traces"} }, "/traces",
		},
	} {
		t.Run(name, func(t *testing.T) {
			base, paths := collector(t)
			cfg := tc.cfg(base)
			cfg.SampleRatio = 1
			provider, err := NewProvider(t.Context(), cfg, log.Settings{}, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			_, span := TracerProvider(provider).Tracer("test").Start(t.Context(), "work")
			span.End()
			if err = provider.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := paths(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("collector paths = %v, want [%s]", got, tc.want)
			}
		})
	}
}

func TestInvalidEndpointFailsTheBuild(t *testing.T) {
	for name, cfg := range map[string]Config{
		"no scheme":       {Endpoint: "collector:4318"},
		"grpc scheme":     {TracesEndpoint: "grpc://collector:4317"},
		"no host":         {Endpoint: "http://"},
		"unparsable":      {Endpoint: "http://[::1"},
		"traces no host:": {TracesEndpoint: "https:///v1/traces"},
	} {
		_, err := NewProvider(t.Context(), cfg, log.Settings{}, slog.New(slog.DiscardHandler))
		if err == nil || !strings.Contains(err.Error(), "endpoint must be an http or https URL") {
			t.Errorf("%s: NewProvider = %v, want the endpoint error", name, err)
		}
	}
}

// TestAppFlushesAProviderThatNeverStarted is the build that fails after
// NewProvider: the App never starts, and its Stop must still flush the spans
// of the failure and stop the exporter.
func TestAppFlushesAProviderThatNeverStarted(t *testing.T) {
	exporter := &recordingExporter{}
	provider, err := newProvider(t.Context(), exportingConfig(), log.Settings{},
		slog.New(slog.DiscardHandler), tracing.WithExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	app := nano.NewApp("billing", nano.AppConfig{StopTimeout: 5 * time.Second})
	app.SetLogger(slog.New(slog.DiscardHandler))
	app.Manage("*nanotrace.Provider", provider)
	app.Manage("*nanotrace.Installer", NewInstaller(provider))
	_, span := TracerProvider(provider).Tracer("test").Start(t.Context(), "failed ping")
	span.End()

	if err = app.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if exporter.shutdowns != 1 || len(exporter.names) != 1 {
		t.Fatalf("shutdowns = %d, spans = %v; want the provider flushed and stopped once", exporter.shutdowns,
			exporter.names)
	}
}
