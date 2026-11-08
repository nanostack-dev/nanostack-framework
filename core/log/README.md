# core/log

`log/slog` is the only logging API in Nanostack. Call sites use `slog.InfoContext(ctx, ...)` or an injected `*slog.Logger`. There is no custom logger type. This package supplies the parts the standard library leaves to the application. zerolog is removed, and the lint standard bans importing it (see [Banning zerolog](#banning-zerolog)).

The package imports only the standard library, `core/secret` and `core/fault`, never a third-party module. OpenTelemetry stays in the tracing package, which passes its trace-id extractor in through `Config.ContextAttrs`.

| API | Purpose |
|---|---|
| `ConfigFromEnv(getenv)`, `Config`, `New`, `NewHandler` | The process handler: JSON in production, text on a development terminal, level from `LOG_LEVEL`, source locations, service identity, `Config.ContextAttrs` (`trace_id`/`span_id` from `tracing.LogAttrs`), and an optional `Export` handler fanned out with `slog.NewMultiHandler` |
| `WithScope`, `SetAttrs`, `WithAttrs`, `Attrs`, `RequestID` | Attributes carried on the context and written by the handler on every line |
| `Key*` constants | The shared keys: `request_id`, `component`, `org_id`, `workspace_id`, `user_id`, `error`, `trace_id`, `span_id` |
| `secret.IsKey` | Redaction: a handler guard on secret-looking keys that writes `secret.Redacted` (`[REDACTED]`); credentials themselves are held as [`secret.Secret`](../secret/secret.go) (see [Redaction](#redaction)) |
| `Err`, `Error`, `ErrorTo`, `LevelFor`, `IsContextError` | Log an error at the level its kind deserves |
| `HTTPMiddleware`, `HTTPOptions`, `RequestIDHeader` | Request id, request scope and one summary line per request, panics and hijacked connections included |
| `Wrap` | The context and redaction stages around any handler, plus optional context extractors such as `tracing.LogAttrs` |
| `logtest.New`, `logtest.Capture`, `Recorder` | Capture records in tests; safe with `t.Parallel` and `testing/synctest` |

## Wiring

```go
cfg, err := log.ConfigFromEnv(os.Getenv)
if err != nil {
	return err
}
cfg.ContextAttrs = tracing.LogAttrs // optional: trace_id and span_id on every output line
cfg.Export = otelHandler            // optional: from the otel module, for example otelslog.NewHandler
slog.SetDefault(log.New(cfg))
```

`slog.SetDefault` also routes the standard library's `log` package through the handler.

Middleware order, outermost first:

```go
handler := tracing.Middleware()(        // starts the server span
	log.HTTPMiddleware(log.HTTPOptions{ // request id, scope, summary line
		SkipSummary: func(r *http.Request) bool { return r.URL.Path == "/health" },
	})(mux))                            // the ServeMux sets r.Pattern
```

`HTTPMiddleware` needs no recovery middleware outside it to log a panic: it writes the summary line at Error with `panic=<value>` and status 500 (or the status already written), then panics again with the same value, so `net/http` or an outer recovery middleware handles it as before, with the original stack. `SkipSummary` does not hide a panic.

A failed request gives two Error lines on purpose, and they answer different questions. `request failed`, from `problem.Write` or `log.Error`, holds the error and its cause: it is the failure. `http request`, the summary, holds the route, status, latency and client: it is the access record, and it is Error on 5xx so that a 5xx that no handler logged is still visible. Alert on failures with `msg="request failed"` at Error, and on the error rate with the summary's `status` (`status>=500`). Do not count every Error line: that counts each logged failure twice. Rejected: an Info summary when the handler already logged an error. It hides the 5xx from a query on the summary's level and couples the middleware to every error helper.

`client_ip` comes from `X-Forwarded-For` or `X-Real-Ip` only when the connection comes from a trusted proxy: `HTTPOptions.TrustedProxies`, by default the loopback and private networks where a cluster's ingress runs. The middleware takes the rightmost hop that is not a trusted proxy, so a client cannot choose its `client_ip` by sending the header itself. Set `TrustedProxies` to the ingress network when it is public, or to an empty slice to always log the connection address.

The middleware's writer keeps `http.Flusher`, `http.Hijacker`, `io.ReaderFrom` and `Unwrap`, so SSE, WebSocket upgrades (gorilla/websocket type-asserts `http.Hijacker`) and sendfile keep working. A hijacked request is logged with `hijacked=true` and status 101.

Auth middleware inside the mux records the caller once with `log.SetAttrs(ctx, slog.String(log.KeyOrgID, ...), slog.String(log.KeyUserID, ...))`. Every later line of the request and the summary line carry those attributes.

### Records pass through three stages

```
context attrs (scope + WithAttrs) → redaction → slog.NewMultiHandler(
    Config.ContextAttrs (trace_id/span_id) + service/env/version → JSON or text output,
    Export (otelslog: reads the span from ctx and the service from its resource))
```

The redaction stage resolves each `slog.LogValuer` once and passes the resolved value on, so a lazy value costs one `LogValue` call per record.

## Redaction

Two controls keep credentials out of log lines.

**`secret.Secret`** (package [`core/secret`](../secret/secret.go)) is the primary control. It is the framework's one credential type: `core/config` binds secret settings as `secret.Secret` too. Hold a credential as `secret.Secret` from the point it is read: config fields, tokens parsed from headers, generated passwords. slog, `fmt` (every verb), `encoding/json` v1 and v2, YAML and any other `encoding.TextMarshaler`-aware encoder print it as `secret.Redacted` (`[REDACTED]`), and an unset one as an empty string, so a config struct logged with `slog.Any("cfg", cfg)` is safe in both formats. The value sits behind a pointer, so a `secret.Secret` in an unexported field, which `fmt` prints by reflection, shows an address, never the value. It still decodes from a JSON, YAML or environment string. Call `Reveal()` only where the raw value is used.

**The key guard** catches mistakes. The handler replaces with `secret.Redacted` the value of every attribute whose key `secret.IsKey` matches: keys ending, ignoring case and `_ - . ` separators, in `password`, `passwd`, `passphrase`, `secret`, `token`, `apikey`, `authorization`, `cookie`, `credential(s)`, `signature`, `privatekey`, `secretkey`, `accesskey`, `signingkey`, `encryptionkey`, `hmackey`, `clientkey`, `sessionkey`, `masterkey`, `dsn`, `connectionstring`, `databaseurl` or `dburl`. A bare `key` is not a secret (`cache_key`, `idempotency_key`), nor is an identifier such as `api_key_id`, `access_key_id` or `secret_name`. The guard applies at any depth of:

- slog groups, and `slog.LogValuer` values that resolve to groups;
- maps with string keys logged with `slog.Any`: `http.Header`, `url.Values`, `map[string]string`, `map[string]any` and any other string-keyed map, which become groups with sorted keys, so `slog.Any("headers", r.Header)` writes `"Authorization":"[REDACTED]"`;
- the `query` group of the request summary line, which the middleware redacts itself, whatever logger writes the line.

### What redaction does not cover

The guard reads keys, never values. These reach the log line verbatim:

- **Values under a public key.** `slog.String("note", "password is hunter2")` is written as is.
- **Error strings.** A driver error that embeds the connection string, such as `connect postgres://user:pass@host/db: ...`, logged with `log.Err(err)` or `log.Error`, is written whole. Wrap such errors at the data layer with a message that leaves the DSN out.
- **URLs.** `slog.String("db", u.String())` and `slog.Any("db", u)` print the password in `user:pass@` and in query parameters such as `?password=`. Log `secret.RedactURL(u)`, which replaces both with `secret.Redacted`. Do not use `u.Redacted()`: it hides only the `user:pass@` password, and writes a second marker, `xxxxx`. The request summary logs `path` raw too, so do not put tokens in paths.
- **Struct fields that are not `Secret`.** `slog.Any("cfg", cfg)` prints a plain `Password string` field. The guard does not reflect over structs; use `secret.Secret` fields or a `LogValue` method.
- **Slices of maps or structs.** `slog.Any("hops", []map[string]string{...})` is encoded whole; only a map at an attribute's top level, or nested in maps and groups, is converted.
- **Request bodies.** Never logged.

Context attributes stay at the top level under `Logger.WithGroup`, so a query on `request_id` finds every line. When a call site logs a key that the context also carries, the call site's value wins and the key appears once. A key set with `Logger.With` is not checked against the context; do not put context keys on loggers.

### Scope or branch

| Use | When | Example |
|---|---|---|
| `SetAttrs(ctx, ...)` | A fact about the whole unit of work, learned partway through it. Writes into the scope that `HTTPMiddleware` (or a worker, with `WithScope`) opened, so lines written later by outer code see it. | auth resolves the organization |
| `WithAttrs(ctx, ...)` | A fact about one branch. Returns a new context; the parent and siblings do not see it. | the job id of one message, the tenant of one row in a cron, the component of a loop |

A queue worker or cron opens a scope per job or tick: `ctx = log.WithScope(ctx)`.

## Migrating from zerolog

The left column lists every zerolog and `pkg/log` idiom in echopoint at `c6f1cb27`, with its approximate count in non-test code.

### Writing lines

| zerolog / pkg/log | slog | Notes |
|---|---|---|
| `log.Ctx(ctx).Info().Str("flow_id", id).Msg("flow created")` (227 `log.Ctx`) | `slog.InfoContext(ctx, "flow created", slog.String("flow_id", id))` | Same for `Debug`, `Warn`, `Error`. Always pass ctx. |
| `.Str(k, v)` (211) | `slog.String(k, v)` | Use typed attrs, not `k, v` pairs (enforced by sloglint, below). |
| `.Int(k, v)` / `.Int64` / `.Int32` / `.Bool` / `.Float64` | `slog.Int` / `slog.Int64` / `slog.Int(k, int(v))` / `slog.Bool` / `slog.Float64` | slog has no `Int32`. |
| `.Err(err)` (201) | `log.Err(err)` | Key stays `error`. |
| `.Dur(k, d)` | `slog.Duration(k, d)` | JSON unit changes from milliseconds (float) to nanoseconds (integer). |
| `.Time(k, t)` | `slog.Time(k, t)` | |
| `.Interface(k, v)` | `slog.Any(k, v)` | |
| `.Msgf(format, args...)` (8) | `slog.InfoContext(ctx, "fixed message", slog.String(...))` | Put the variable parts in attributes so the message stays constant and groupable. |
| `.Send()` | a real message | slog always takes a message. |
| `logger.WithLevel(lvl).…Msg(m)` | `logger.LogAttrs(ctx, lvl, m, attrs...)` | |
| `zerolog.Level`, `zerolog.InfoLevel`, … | `slog.Level`, `slog.LevelInfo`, … | |
| `logger.Printf(format, args...)` on a zerolog logger (3, `dbgen.go`) | `fmt.Printf` in a CLI tool, or `slog.Info` with attributes | |
| `zerolog.Arr()` + `zerolog.Dict()` (openapisyncs cron) | `slog.Any("triggered_runs", runs)` with a slice of structs, or a type that implements `slog.LogValuer` | See "No clean equivalent". |
| `.Fatal()` (9, in `main` functions and `dbgen.go`) | `slog.Error(...)` then `os.Exit(1)`, or return the error from `run()` | See "No clean equivalent". |

### Errors

| zerolog / pkg/log | slog |
|---|---|
| `log.Error(ctx, err).Msg("load flow")` (~250) | `log.Error(ctx, err, "load flow")` |
| `log.Error(ctx, err).Str("flow_id", id).Msg(m)` | `log.Error(ctx, err, m, slog.String("flow_id", id))` |
| `log.Event(logger, err).Msg(m)` | `log.ErrorTo(ctx, logger, err, m)` |
| `log.LevelFor(err)` | `log.LevelFor(err)`, now a `slog.Level` |
| `log.IsContextError(err)` | `log.IsContextError(err)` |

`LevelFor` applies these rules in order: cancellation and deadline are Warn; an error with a `Level() slog.Level` method anywhere in its chain picks its own level; an error with a `ProblemStatus()` from 400 to 499 (the status method of `core/fault`'s `Described`, which `pkg/fault.Error` also has) is Debug; everything else is Error. Two points differ from `pkg/log`, which made every `ProblemStatus() < 500` Debug: the `Level()` method is new, and a status below 400, such as 200 or 302, is now Error, because only 400 to 599 counts as an HTTP error status (`fault.IsErrorStatus`). A `core/fault` `Described` with an empty `ProblemType()` also counts as no status (`fault.Valid`), as the problem writer renders it as a 500; the legacy `pkg/fault.Error`, which has no `ProblemType()`, needs only the status. `LevelFor` does not import `pkg/search` or `pkg/db/pgerr`; such errors choose their level with `Level()`. Two changes follow before the migration:

- `pkg/search` client errors (`ErrInvalidCursor`, `ErrCursorWithOffset`, `ErrUnknownSort`) get a `Level()` method that returns `slog.LevelDebug`, or a `ProblemStatus()` method that returns 400.
- The data layer wraps lib/pq's "canceling statement due to user request" (SQLSTATE 57014) with `context.Canceled`, so `IsContextError` matches it.

### Context and loggers

| zerolog / pkg/log | slog |
|---|---|
| `log.Bind(ctx)` (8) | Delete. The handler reads the context when it writes a line, so nothing needs rebinding. |
| `log.WithComponent(ctx, "flow_schedule_fire")` then `log.Bind` | `ctx = log.WithAttrs(ctx, slog.String(log.KeyComponent, "flow_schedule_fire"))` |
| `log.Enricher` / `logging.EnricherGroup` (auth `LogEnricher`) | The code that sets the auth context also calls `log.SetAttrs` (request) or `log.WithAttrs` (a cron that adopts an organization). A helper in the product, such as `contextkeys.WithAuth(ctx, a)`, does both. |
| `requestlog.NewLogEnricher` | Delete. `HTTPMiddleware` puts `request_id`, `method` and `path` in the scope. |
| `requestlog.From(ctx).UpdateContext(...)` | `log.SetAttrs(ctx, ...)` |
| `requestlog.RequestIDFromContext(ctx)` | `log.RequestID(ctx)` |
| `log.Op(ctx)` / `log.OpNamed(ctx, name)` | The `source` group of every JSON line already names the function. For an explicit name, `slog.String("operation", name)`. |
| `log.Ctx(ctx).With().Int64("job_id", id).Logger()` then `logger.Info()…` | `ctx = log.WithAttrs(ctx, slog.Int64("job_id", id))`, then `slog.InfoContext(ctx, ...)` |
| injected `zerolog.Logger` field | `*slog.Logger` field |
| `logger.With().Str("component", x).Logger()` (12 constructors) | `logger.With(slog.String(log.KeyComponent, x))` |
| `zerolog.Nop()` | `slog.New(slog.DiscardHandler)` |
| `zerolog.New(os.Stdout).With().Timestamp().Logger()` (`dbgen.go`, test setup) | `slog.New(slog.NewTextHandler(os.Stderr, nil))`, or `log.New(cfg)` |
| `.Caller()`, `.Timestamp()` | Built in: `source` and `time`. |
| `logging.NewLoggingConfig`, `NewZerologLogger`, `log.Install`, `log.Binder` | `log.ConfigFromEnv`, `log.New`, `slog.SetDefault` |
| `logging.WithFxLogger` / `pkg/fxlog` | Removed with fx. |

### HTTP

| pkg/httputil/requestlog | core/log |
|---|---|
| `requestlog.Contextualize(base)` + `requestlog.New(log, opts)` / `NewFromEnv` | `log.HTTPMiddleware(log.HTTPOptions{...})` |
| `log.TraceEnricher` | `cfg.ContextAttrs = tracing.LogAttrs` |
| first `X-Forwarded-For` hop, always trusted | rightmost hop not in `HTTPOptions.TrustedProxies` (default: loopback and private networks) |
| `Options.Skip` (echopoint skips `/health` and `/runner/jobs/next`) | `HTTPOptions.SkipSummary`. The request still gets its id and scope. |
| `Options.LogRequestBody`, `LOG_REQUEST_BODY` | Removed. A body is where passwords and tokens travel, and key redaction cannot see inside it. |

### Tests

| zerolog | logtest |
|---|---|
| `zerolog.New(&buf)` + `json.Unmarshal` per line (queuestats, middleware, auth enricher tests) | `logger, rec := logtest.New()`, then `rec.AssertLogged(t, slog.LevelWarn, "msg", slog.String("queue", "jobs"))` |
| `ctx := zerolog.New(&buf).WithContext(ctx)` | `ctx, rec := logtest.Capture(t)` |
| `sdk.CaptureLogs(t)` with `log.Install` ("callers must not run in parallel") | `ctx, rec := logtest.Capture(t)`; parallel tests are safe because records route by context |

Do not call `slog.SetDefault` in a test package that uses `logtest.Capture`.

## Output changes that affect dashboards and alerts

Update the Grafana queries, alert rules and gate flows that parse log lines in the same change that migrates a product.

| Field | zerolog | slog |
|---|---|---|
| message | `message` | `msg` |
| level | `"level":"info"` | `"level":"INFO"` |
| caller | `"caller":"/app/x.go:42"` | `"source":{"function":…,"file":…,"line":42}` |
| durations | milliseconds, float | nanoseconds, integer |
| request summary message | `GET /flows/123 -> 200 (2.5ms)` | `http request`, a constant; the details are attributes, and the new `route` attribute holds the pattern |
| request summary level | Error on 5xx | also Warn, with `client_canceled=true`, when the client disconnected |
| `operation` | from `log.Op` | gone; use `source.function` |
| request summary `query` | the raw query string | a group, one attribute per parameter (`query.page`), secret parameters `[REDACTED]` |
| request summary on panic | no line | Error, `panic=<value>`, status 500 or the status already written |
| request summary on hijack | status 200 | status 101, `hijacked=true` |
| `client_ip` | first `X-Forwarded-For` hop | rightmost hop not from a trusted proxy |
| a map logged with `slog.Any` | `map[k:v]` in text | a group in text (`headers.Accept=[json]`); JSON unchanged except redacted entries |

## No clean equivalent

- **`.Fatal()`.** slog has no fatal level. Log at Error and call `os.Exit(1)` in `main`, or better, return the error from a `run` function. A library never exits.
- **`zerolog.Arr()` of `zerolog.Dict()`.** slog has groups but no arrays of groups. `slog.Any` with a slice of structs encodes correctly in JSON but prints `%+v` in text; a `slog.LogValuer` per element is the typed alternative.
- **A zerolog event without `.Msg`.** `dbgen.go:164` calls `log.Fatal().Err(err)` without `.Msg`, so it never logged and never exited. The slog version logs and exits; check that this is the intended behaviour.
- **`.Msgf`.** A formatted message works with `fmt.Sprintf`, but it defeats message grouping. Move the variable parts to attributes.
- **`.Int32`, `.Uint` sizes.** slog has `Int`, `Int64`, `Uint64` and `Float64`; convert.
- **Duplicate keys.** zerolog wrote every `.Str` call. slog's JSON handler also writes duplicates from `Logger.With`. This package removes duplicates between context attributes and call-site attributes only.
- **Enrichers that read product context lazily.** The handler cannot read a product's context types. The product sets attributes where it sets its context. This is explicit and also covers background paths, which `log.Bind` covered only when someone remembered to call it.
- **Error classification that needs a driver.** `LevelFor` cannot recognise lib/pq's cancelled statement without importing the driver. The data layer must wrap it (see Errors).
- **Request body logging.** Removed on purpose; see HTTP.

## Banning zerolog

The lint standard in [`lint/base.golangci.yml`](../../lint/base.golangci.yml) enforces the decision:

- depguard's `deprecated` rule denies `github.com/rs/zerolog` in every file, tests included. The entry matches by prefix, so it also bans `zerolog/log`, `zerolog/pkgerrors` and the rest.
- depguard's `context-scoped logging` rule denies the standard `log` package outside tests and points at `log/slog` with a context.
- `sloglint` keeps slog calls in one shape outside tests (settings below). Tests are excluded because they log odd shapes on purpose and assert on dotted group paths such as `query.api_key`.

A service still on zerolog sees the ban when it upgrades the framework. It migrates, or excludes each file with one rule per file under its exclusion budget (see [`lint/README.md`](../../lint/README.md)), so the remaining files are counted. The framework does the same for its own legacy packages (`pkg/log`, `modules/logging` and the other zerolog importers), which are deleted when the products have migrated.

When `nanovet` lands, the same rule becomes an analyzer that also covers code outside golangci-lint:

```go
// lint/nanovet/nozerolog/analyzer.go
var Analyzer = &analysis.Analyzer{
	Name: "nozerolog",
	Doc:  "reports imports of github.com/rs/zerolog: log/slog is the only logging API",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			for _, spec := range file.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if path == "github.com/rs/zerolog" || strings.HasPrefix(path, "github.com/rs/zerolog/") {
					pass.Reportf(spec.Pos(), "import of %s: use log/slog (see core/log/README.md)", path)
				}
			}
		}
		return nil, nil
	},
}
```

The base configuration enables `sloglint` with:

```yaml
sloglint:
  attr-only: true       # slog.String(k, v), never "k", v pairs
  context: scope        # pass ctx wherever one is in scope
  static-msg: true      # no fmt.Sprintf messages
  key-naming-case: snake
  forbidden-keys: [time, level, msg, source]
```

Still to add: a ruleguard rule in `gorules/` that catches errors logged at a fixed level. It is not in `gorules/` yet, and a service enables it by listing the file in its `ruleguard.rules`:

```go
// errorLevelFromKind reports an error logged at a fixed Error level, which
// pages on cancellations and 4xx answers.
func errorLevelFromKind(m dsl.Matcher) {
	m.Import("log/slog")
	m.Match(
		`slog.ErrorContext($ctx, $msg, $*_, slog.Any("error", $err), $*_)`,
		`slog.ErrorContext($ctx, $msg, $*_, log.Err($err), $*_)`,
	).Report(`use log.Error($ctx, $err, $msg, ...): it picks the level from the error`)
}
```
