package tracing_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		tracing.EnvSDKDisabled, tracing.EnvTracesExporter, tracing.EnvExporterEndpoint,
		tracing.EnvTracesEndpoint, tracing.EnvTracesSamplerArg, tracing.EnvServiceNameFallback,
		tracing.EnvServiceVersionSource, "OTEL_SERVICE_NAME", "OTEL_RESOURCE_ATTRIBUTES",
	} {
		t.Setenv(key, "")
	}
}

func TestConfigFromEnvEnabled(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "no endpoint", want: false},
		{name: "generic endpoint", env: map[string]string{tracing.EnvExporterEndpoint: "http://otel-collector:4318"}, want: true},
		{name: "traces endpoint", env: map[string]string{tracing.EnvTracesEndpoint: "http://otel-collector:4318/v1/traces"}, want: true},
		{name: "sdk disabled", env: map[string]string{
			tracing.EnvExporterEndpoint: "http://otel-collector:4318", tracing.EnvSDKDisabled: "true",
		}, want: false},
		{name: "sdk disabled false", env: map[string]string{
			tracing.EnvExporterEndpoint: "http://otel-collector:4318", tracing.EnvSDKDisabled: "false",
		}, want: true},
		{name: "exporter none", env: map[string]string{
			tracing.EnvExporterEndpoint: "http://otel-collector:4318", tracing.EnvTracesExporter: "none",
		}, want: false},
		{name: "blank endpoint", env: map[string]string{tracing.EnvExporterEndpoint: "  "}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := tracing.ConfigFromEnv().Enabled; got != tc.want {
				t.Fatalf("Enabled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigFromEnvSampleRatio(t *testing.T) {
	cases := map[string]float64{"": 1, "0.25": 0.25, "0": 0, "1": 1, "1.5": 1, "-0.1": 1, "half": 1}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tracing.EnvTracesSamplerArg, raw)
			if got := tracing.ConfigFromEnv().SampleRatio; got != want {
				t.Fatalf("SampleRatio(%q) = %v, want %v", raw, got, want)
			}
		})
	}
}

func TestConfigFromEnvServiceFallbacks(t *testing.T) {
	clearEnv(t)
	t.Setenv(tracing.EnvServiceNameFallback, "anchor")
	t.Setenv(tracing.EnvServiceVersionSource, "v1.2.3")
	cfg := tracing.ConfigFromEnv()
	if cfg.ServiceName != "anchor" || cfg.ServiceVersion != "v1.2.3" {
		t.Fatalf("fallbacks = %q/%q", cfg.ServiceName, cfg.ServiceVersion)
	}
}
