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

Low-level worker lifecycle primitives currently live in `pkg/queueworker` so they can be reused without depending on FX.
