# 0018: slog is the only logging API

- Status: proposed
- Date: 2026-11-07

## Context

The framework logs with zerolog. It appears in 44 files across `modules/` (cache, config, httpserver, logging, migrations, pglock, postgres, pprof, tracing) and `pkg/` (fxlog, httputil/requestlog, log, queueworker, redisx, testkit/searchtest). `pkg/log` wraps it: it binds a logger to the request context, and `Enricher` functions add fields such as the component and the trace and span ids (2026-10-24 "OpenTelemetry Tracing In The Framework" in the [decision log](../decision-log.md)). The lint standard's `depguard` rule refuses the standard `log` package and the zerolog global logger in favour of `pkg/log`.

The standard library has `log/slog` with a JSON handler, and Go 1.26 added `slog.NewMultiHandler`, which fans one record out to several handlers. The OpenTelemetry `otelslog` bridge is a slog handler. Code written today, by people and agents, reaches for `slog` first, so a zerolog codebase gets two idioms.

## Decision

`*slog.Logger` is the only logging API in the framework and in products.

- Output is the standard library JSON handler. Key names and level names are fixed once in framework handler options.
- A framework context handler adds request-scoped fields (trace id, span id, component) from the context. It replaces `pkg/log`'s `Enricher` and `Bind`.
- OpenTelemetry logs are exported by adding the `otelslog` handler through `slog.NewMultiHandler`.
- zerolog is removed from every `go.mod`. The `depguard` rule in the lint standard refuses `github.com/rs/zerolog` and keeps refusing the standard `log` package.

## Alternatives

- **Keep zerolog.** Lower allocation per log line. Rejected because it keeps a second idiom beside the standard library, and OpenTelemetry and other handlers plug into slog, not zerolog.
- **slog API with a zerolog handler underneath.** One API, but a dependency kept for a performance margin that no service has shown it needs.
- **zap.** Same trade as zerolog: a third-party API beside the standard one.
- **Both during a long transition.** Rejected as an end state. The move happens in one phase with the lifecycle work.

## Consequences

- Every framework constructor that takes a `zerolog.Logger` takes a `*slog.Logger`. This breaks module APIs and lands with the lifecycle change from [ADR 0013](0013-generated-wiring-replaces-fx.md).
- The JSON keys change unless the handler options keep the current ones (zerolog writes `message` and lowercase levels; slog writes `msg` and `INFO`). Check product log queries, dashboards and log alerts before rollout, and choose the key names once.
- slog's JSON handler allocates more than zerolog per line. A hot path that shows it in a profile can log less or use `LogAttrs`.
- `pkg/fxlog` goes with fx, and `pkg/log` shrinks to the context handler and shared field names.
