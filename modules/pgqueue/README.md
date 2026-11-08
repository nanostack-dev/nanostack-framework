# pgqueue

This directory has no code, and the framework has no queue module.

pgkit owns the lifecycle of its queue, its workflows and its admin UI. Its fx wiring is in pgkit (`pgkit/fx/queue`, `pgkit/fx/workflow`, `pgkit/fx/adminui`). For generated wiring, the pgkit types implement the `core/nano` lifecycle by method shape (`Start`, `Stop`, `Run`). The `core/nano` interfaces are structural, so pgkit does not import the framework. A service lists the pgkit constructors in its manifest with `nano.Providers`. While a service is still on fx, `modules/nanofx.Lifecycle[T]` runs a pgkit type in the fx graph (see [docs/modules.md](../../docs/modules.md)).

The service owns:

- queue names
- payload schemas
- handler logic
- retry policy and max attempts
- idempotency and decisions about external side effects

pgkit carries the trace context across the queue: the enqueue context, then the stored `traceparent`, then the handler context. When the handler context has the span, `otel/nanotrace` adds `trace_id` and `span_id` to each log line, and PostgreSQL queries become child spans. No framework hook is necessary.

`pkg/queueworker` keeps the low-level worker lifecycle primitives, which have no fx dependency.
