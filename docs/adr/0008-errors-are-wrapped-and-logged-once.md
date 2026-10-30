# 0008: Errors are wrapped on the way up and logged once

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

The [strict error handler](../../modules/httpserver/strict_error_handler.go) already logs every error a strict handler returns, once, on the request logger:

- a 5xx at error level, with `error_type` and the `%+v` wrapped chain;
- a 4xx at info level.

Its comment calls this line "the safety net for failures a source layer forgot to log". Source layers don't forget, though: in echopoint the repository, the service and the handler each log the same failure. One failed lookup therefore produces up to four lines.

## Decision

1. **Create an error where its meaning is known.** That is a domain sentinel (`flow.ErrVersionOfAnotherFlow`), or a `fault` constructor in the feature's `errors.go` (`NewFlowNotFoundError` → 404 `FLOW_NOT_FOUND`).
2. **Every other layer wraps with short context and `%w`**, or returns the error unchanged. Write `"find flow %s: %w"`, not `"failed to find flow"`. Identifiers needed for support go into that context.
3. **Log once, at the boundary.** The strict error handler is the primary log for HTTP, not a safety net. The worker boundary is the job row and its span, until OQ-3 decides on a log line.
4. **An action may log one `Info` line** for a business event worth auditing. Nothing else logs errors.

```go
// before: the repository logs and returns; the service and handler do the same
found, err := transactor.QueryOptionalMap(ctx, r.db, stmt, r.flowMapper.ToDomain)
if err != nil {
	log.Ctx(ctx).Error().Err(err).Str("flow_id", flowID.String()).Msg("failed to find flow")
	return functional.None[flow.Flow](), err
}

// after: wrap on the way up; the strict error handler logs it once
if err != nil {
	return flow.Flow{}, fmt.Errorf("find flow %s: %w", id, err)
}
```

## Alternatives

- **Log and return at every layer (status quo).** Each layer adds context, but as separate lines that support has to join by request id. Wrapping puts the same context into one line, in order. Dave Cheney notes that logging and returning "produces duplicate log lines" ([source](https://dave.cheney.net/2016/04/27/dont-just-check-errors-handle-them-gracefully)). Uber's guide says to "handle each error only once" and to drop "failed to" ([Uber](https://github.com/uber-go/guide/blob/master/style.md#handle-errors-once)).
- **Wrap database errors with `%v` to hide them.** The Go blog advises this when exposing errors to outside callers ([Go blog](https://go.dev/blog/go1.13-errors)). Inside a service, `%w` is better, because the boundary and `pgerr` classification must still recognise the cause.

## Consequences

- One line per failure carries the request id, status, type and full chain. Functions also get shorter.
- `fault.Error.Error()` leaves out `Meta`, so fault metadata does not reach the boundary line today. Either put identifiers in the wrap context, or extend the strict error handler to log `Meta` (OQ-3).
- pgkit stores a failed job's error on the job and records it on its span, but writes no log line per attempt. The worker boundary needs the same once-only rule (OQ-3).
- If accepted, the strict error handler's comment changes from "safety net" to "the error log".
