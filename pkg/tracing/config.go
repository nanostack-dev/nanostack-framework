// Package tracing builds the OpenTelemetry tracer provider and the HTTP
// instrumentation Nanostack services share, so one request can be followed as a
// single trace across the edge proxy and every service it reaches.
//
// The package has no fx dependency; modules/tracing wires it into a service.
package tracing

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// Environment variables read by ConfigFromEnv. Except for SERVICE_NAME and
// SERVICE_VERSION, which modules/logging already reads, they are the standard
// OpenTelemetry SDK variables, so infrastructure configures them once for every
// service.
const (
	EnvSDKDisabled          = "OTEL_SDK_DISABLED"
	EnvTracesExporter       = "OTEL_TRACES_EXPORTER"
	EnvExporterEndpoint     = "OTEL_EXPORTER_OTLP_ENDPOINT"
	EnvTracesEndpoint       = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	EnvTracesSamplerArg     = "OTEL_TRACES_SAMPLER_ARG"
	EnvServiceNameFallback  = "SERVICE_NAME"
	EnvServiceVersionSource = "SERVICE_VERSION"
)

const defaultSampleRatio = 1.0

// Config selects whether spans are exported and how they are sampled.
//
// The exporter itself (endpoint, headers, timeout, compression) and the
// resource (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES) are configured by the
// standard OTEL_* variables the SDK reads directly; Config only carries the
// decisions the framework makes on top of them.
type Config struct {
	// Enabled reports whether an exporting provider is built. When false, New
	// returns a no-op provider and nothing leaves the process.
	Enabled bool
	// SampleRatio is the TraceIDRatio fraction applied to new root traces. An
	// incoming sampled parent (from traceparent) is always honoured.
	SampleRatio float64
	// ServiceName is a fallback for service.name when OTEL_SERVICE_NAME and
	// OTEL_RESOURCE_ATTRIBUTES do not set one.
	ServiceName string
	// ServiceVersion is a fallback for service.version, in the same way.
	ServiceVersion string
}

// ConfigFromEnv reads Config from the process environment.
//
// Export is enabled only when an OTLP endpoint is configured
// (OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT), the SDK
// is not disabled (OTEL_SDK_DISABLED=true) and the traces exporter is not
// "none". Local development and tests therefore need no collector.
//
// SampleRatio comes from OTEL_TRACES_SAMPLER_ARG and defaults to 1.0; values
// outside [0, 1] or unparsable values fall back to the default.
func ConfigFromEnv() Config {
	return Config{
		Enabled:        exportEnabled(),
		SampleRatio:    sampleRatio(os.Getenv(EnvTracesSamplerArg)),
		ServiceName:    strings.TrimSpace(os.Getenv(EnvServiceNameFallback)),
		ServiceVersion: serviceVersion(),
	}
}

func exportEnabled() bool {
	if disabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(EnvSDKDisabled))); err == nil && disabled {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(EnvTracesExporter)), "none") {
		return false
	}
	return strings.TrimSpace(os.Getenv(EnvExporterEndpoint)) != "" ||
		strings.TrimSpace(os.Getenv(EnvTracesEndpoint)) != ""
}

func sampleRatio(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSampleRatio
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio < 0 || ratio > 1 {
		return defaultSampleRatio
	}
	return ratio
}

func serviceVersion() string {
	if version := strings.TrimSpace(os.Getenv(EnvServiceVersionSource)); version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	return info.Main.Version
}
