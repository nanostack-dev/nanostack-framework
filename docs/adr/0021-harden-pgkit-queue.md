# 0021: Harden pgkit/queue using River's design

- Status: proposed
- Date: 2026-11-07

## Context

`pgkit/queue` is the framework's durable job queue: jobs live in PostgreSQL, enqueue joins the business transaction, workers claim with `SKIP LOCKED` and wake on `NOTIFY`, and typed handles use generic methods (`c.Queue[SendInvoice]("invoice.send")`). pgkit also holds durable cron (`pgcron`) with missed-run coalescing and a versioned workflow engine. It is about 5.6k lines of code and 4.7k lines of tests. `modules/pgqueue` wires it into services, and queue trace propagation was left to it (2026-10-24 "OpenTelemetry Tracing In The Framework" in the [decision log](../decision-log.md)).

The queue has four states: `pending`, `processing`, `done` and `failed`. A reaper requeues stuck jobs, `available_at` in the claim query delays retries, a retention cleaner is in progress, and `pgcron` elects a leader with an advisory lock. It has no unique jobs, no snooze or cancel result, and `failed` means both "out of attempts" and "asked to stop".

River is a mature PostgreSQL queue for Go, open source under MPL-2.0, with about 50k lines of code and 47k lines of tests. Its design covers exactly the gaps above. The River features that pgkit's users would want beyond that (durable periodic jobs, workflows, a dead-letter queue, batching, concurrency limits) are in the paid River Pro tier, and the rights end when payment stops.

## Decision

Keep `pgkit/queue`, moved into the monorepo, and raise it to River's design level using River as a reference implementation. No River dependency and no River files.

- **States.** Add `scheduled`, `retryable`, `cancelled` and `discarded`, with an online migration of existing rows. `discarded` (out of attempts) and `cancelled` (asked to stop) become different states.
- **Maintenance services**, run by one elected leader: a scheduler that moves due scheduled and retryable jobs to available in batches, keeping the claim query cheap; a rescuer for jobs whose worker died, with attempt accounting and error recording; and a cleaner with retention per final state and batched deletes.
- **Unique jobs**: a unique key, plus the set of states in which uniqueness applies.
- **Handler results**: snooze and cancel are typed results, beside success and error.
- **Stuck-job handling**: when a handler ignores cancellation past its deadline, the worker logs a goroutine dump.
- **Notify limiter**: `NOTIFY` is debounced under bursts, with polling as the fallback.
- **Conformance suite**: one suite of queue scenarios that runs against every database handle type the framework supports.
- **Scenarios, not files.** River's test scenarios are rewritten in pgkit's own code. Each ported test links the River behaviour it covers, for traceability. No River source file is copied or modified.

## Alternatives

- **Adopt River.** Rejected. Its open-source core covers what this record adds, but the features beyond it are Pro-only and paid, and River would replace a queue that already shares pgkit's transaction, cron and workflow model. Products would rewrite every handler and payload registration.
- **Adopt River Pro.** Rejected: a paid tier whose rights end when payment stops, for features pgkit already has (durable cron, workflows).
- **Copy River's files.** Rejected. MPL-2.0 is file-level copyleft: a copied or modified file stays MPL-2.0, needs its notice added (River's files carry no per-file header), and its source must be offered on distribution. That would matter the day the framework is published.
- **Keep pgkit as it is.** Rejected for the conflated failure state, missing unique jobs and maintenance gaps above.
- **A Redis-backed queue.** Rejected. Durable product work belongs in the database transaction that creates it, as the framework's boundaries require.

## Consequences

- The job table gains states and columns through an online migration, and the admin UI shows the new states and maintenance runs.
- Products need no code change for the hardening itself; the new features are opt-in.
- Back-off, scheduling, rescue and cleaning are tested with `testing/synctest` and a fake clock, and the conformance suite gates any change to how the queue talks to the database.
- The framework carries the maintenance cost of features a dependency would have provided, in exchange for one queue model shared with cron and workflows and no licence obligations.
