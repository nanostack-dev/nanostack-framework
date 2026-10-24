# pgqueue

Design home for pgqueue lifecycle integration.

The framework should own:

- queue client construction and schema ensure hooks when wrapping pgkit directly
- worker lifecycle start/stop helpers
- standard failed/stuck job logging hooks
- dashboard lifecycle wiring when enabled

Applications still own:

- queue names
- payload schemas
- handler logic
- retry policy and max attempts
- idempotency and external side-effect decisions

Trace context across the queue (enqueue context → stored `traceparent` → handler context) is owned by pgkit itself, not by a framework hook. Once a handler's context carries the span, `log.TraceEnricher` adds `trace_id`/`span_id` on `log.Bind` and PostgreSQL queries become child spans with no further framework wiring.

Low-level worker lifecycle primitives currently live in `pkg/queueworker` so they can be reused without depending on FX.
