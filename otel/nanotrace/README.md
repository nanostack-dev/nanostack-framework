# otel/nanotrace

The nano module for OpenTelemetry tracing. It replaces the fx module `modules/tracing`.

- **Provides:** `*Provider` from `NewProvider(ctx, Config, log.Settings, *slog.Logger)`, and `trace.TracerProvider` from `TracerProvider(*Provider)`. `ContextAttrs` is a member of the `[]log.ContextAttrs` collection of `core/logging`, so log lines get `trace_id` and `span_id`.
- **Lifecycle:** `Start` installs the provider and the W3C propagator globally and sends OpenTelemetry errors to the log at Warn. `Stop` flushes buffered spans.
- **Config:** the standard names `OTEL_SDK_DISABLED`, `OTEL_TRACES_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` and `OTEL_TRACES_SAMPLER_ARG` (a ratio from 0 to 1, default 1). Spans are exported only when an endpoint is set, the SDK is not disabled and the exporter is not `none`. The exporter reads its endpoint from the environment, so set these variables in the environment, not in YAML.
- **Identity:** the fallback `service.name` and `service.version` come from `log.Settings` (`SERVICE_NAME`, `SERVICE_VERSION`). `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` win over them.

The provider itself is `pkg/tracing`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
