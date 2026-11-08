# otel/nanotrace

The nano module for OpenTelemetry tracing. It replaces the fx module `modules/tracing`.

- **Provides:** `*Provider` from `NewProvider(ctx, Config, log.Settings, *slog.Logger)`, `trace.TracerProvider` from `TracerProvider(*Provider)` and `*Installer` from `NewInstaller(*Provider)`. `ContextAttrs` is a member of the `[]log.ContextAttrs` collection of `core/logging`, so log lines get `trace_id` and `span_id`.
- **Lifecycle:** `Installer.Start` installs the provider and the W3C propagator globally and sends OpenTelemetry errors to the log at Warn. `Provider.Stop` flushes buffered spans and stops the exporter. The Provider has no `Start`, so the App stops it whenever it was built, even when the build fails after it.
- **Config:** the standard names `OTEL_SDK_DISABLED`, `OTEL_TRACES_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` and `OTEL_TRACES_SAMPLER_ARG` (a ratio from 0 to 1, default 1). Spans are exported only when an endpoint is set, the SDK is not disabled and the exporter is not `none`. The settings bind from the environment or a YAML layer (`tracing.endpoint`, `tracing.traces_endpoint`). The traces endpoint is used as given; the base endpoint gets `/v1/traces` joined to its path, as the OpenTelemetry specification says. An endpoint that is not an http or https URL fails the build. Headers, timeout and compression still come from the `OTEL_EXPORTER_OTLP_*` environment variables.
- **Identity:** the fallback `service.name` and `service.version` come from `log.Settings` (`SERVICE_NAME`, `SERVICE_VERSION`). `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` win over them.

On fx (`modules/nanofx`), register both values, or the global provider is never installed:

```go
nanofx.Context(signalCtx),
fx.Supply(cfg.Tracing, cfg.Log),
fx.Provide(nanotrace.NewProvider, nanotrace.TracerProvider, nanotrace.NewInstaller),
nanofx.Lifecycle[*nanotrace.Provider]("tracing"),
nanofx.Lifecycle[*nanotrace.Installer]("tracing-install"),
```

The provider itself is `pkg/tracing`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
