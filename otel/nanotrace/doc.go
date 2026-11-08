// Package nanotrace is the nano module for OpenTelemetry tracing. It builds
// the tracer provider of pkg/tracing from typed configuration, publishes it
// and the W3C propagator globally when the App starts (Installer), flushes it
// when the App stops (Provider), and adds trace_id and span_id to log lines.
//
// It replaces the deprecated fx module modules/tracing. A service lists
// Module in its manifest, after core/logging.Module, and puts Config in its
// own Config:
//
//	type Config struct {
//		Log     log.Settings     `config:"log"`
//		Tracing nanotrace.Config `config:"tracing"`
//	}
//
// The settings keep the standard OTEL_* environment names, because
// infrastructure sets them once for every service.
package nanotrace
