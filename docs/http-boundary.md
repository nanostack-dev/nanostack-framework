# HTTP boundary

The HTTP boundary is the code between `net/http` and the handlers that oapi-codegen generates. It has three parts:

- `http/nanohttp` decodes request bodies strictly and runs the generated validators.
- `gen/validate` (`nano gen validate`) writes those validators from the OpenAPI document. [Generation](generation.md#openapi) describes the setup.
- `http/problem` renders every error as an RFC 9457 `application/problem+json` response.

Nothing reads the spec at runtime. kin-openapi is used only in tests, to cross-check the generated validators.

## Request decoding

`nanohttp.Decode` runs for every JSON request body, in this order:

1. **Content-Type.** A missing header is read as JSON. A header that does not parse, such as `application/json; charset`, is a 400 `invalid_parameter` problem for the parameter `Content-Type`. A well-formed type that is not `application/json` or `application/*+json` is a 415 `unsupported_media_type` problem. The detail names the parsed media type only and never echoes the raw header.
2. **Size.** The body is read up to the limit. `nanohttp.MaxBodyBytes(limit)` sets the limit for the requests it wraps. It also wraps the body with `http.MaxBytesReader` and the response writer, so past the limit the server closes the connection instead of draining the rest. Without the middleware, `Decode` still enforces `DefaultMaxBodyBytes` (1 MiB). A larger body is a 413 `body_too_large` problem. An empty or blank body is `nanohttp.ErrNoBody`: a 400 `missing_body` problem when the body is required, and a nil body when it is optional.
3. **JSON.** The body is decoded with `encoding/json/v2`. Duplicate member names, invalid UTF-8 and trailing data are rejected, and member names match case-sensitively. A syntax error is a 400 `malformed_json` problem. A value of the wrong JSON type is a 400 `invalid_body` problem with its pointer. An integer field accepts any JSON number whose value is an integer, such as `3.0` or `1e2`, as JSON Schema defines it. `3.5` is a type error, and a value too large for the Go type is a `format` error. Go's integer types get this rule from `validate.DecodeOptions`; a named integer type, such as an integer enum, gets it from the `UnmarshalJSONFrom` method that `nano gen validate` writes for it. A named integer type from another package (`x-go-type`) keeps json/v2's rule, which rejects `3.0`.
4. **Schema.** The generated `Validate` method runs with the document attached, so required members, nulls and `additionalProperties` are checked against the JSON text rather than the Go zero values. A JSON `null` body is a `type` violation at the root pointer `""` unless the body schema accepts null. Every violation comes back at once, as one 422 `validation_failed` problem.

Unknown members are skipped unless the schema sets `additionalProperties: false`. The document index is linear in the size of the body, whatever its shape.

## Problem responses

Every error path ends in `problem.Write`. The response has the media type `application/problem+json` and the header `X-Content-Type-Options: nosniff`.

| Member | Content |
| --- | --- |
| `type` | The problem type URI. |
| `title` | A short summary of the problem type. |
| `status` | The HTTP status. |
| `detail` | An explanation of this occurrence, when one is safe to send. |
| `instance` | The request path. |
| `code` | A stable machine-readable code, such as `http.validation_failed`. |
| `trace_id` | The trace of the request, when it has one. |
| `errors` | Every violation, for validation, decoding and parameter problems. |

Each item of `errors` locates its violation with exactly one of two members:

```json
{"pointer": "/headers/0/name", "code": "pattern", "detail": "must match the pattern ^[A-Za-z0-9-]+$"}
{"parameter": "limit", "code": "maximum", "detail": "must be at most 100"}
```

`pointer` is an RFC 6901 JSON Pointer into the request body; `""` is the whole body. `parameter` names a path, query, header or cookie parameter. `code` is the JSON Schema keyword that failed (`required`, `type`, `format`, `enum`, `minLength`, `pattern`, `oneOf`, `additionalProperties`, ...) or, for malformed JSON, one of `syntax`, `duplicate_name`, `invalid_utf8` and `trailing_data`. Package `core/validate` declares every code as a `Code...` constant, so Go clients and tests compare against the constants. `detail` never echoes the value.

The framework's problem types are `https://errors.nanostack.dev/http/<name>`:

| Name | Status | When |
| --- | --- | --- |
| `validation_failed` | 422 | The body decoded but violates the schema. |
| `invalid_body` | 400 | A body value has the wrong JSON type or is out of range. |
| `malformed_json` | 400 | The body is not one valid JSON text. |
| `missing_body` | 400 | A required body is empty. |
| `body_too_large` | 413 | The body is over the limit. |
| `unsupported_media_type` | 415 | The body is not JSON. |
| `invalid_parameter` | 400 | A parameter, or the Content-Type header, is missing, malformed or violates its schema. |
| `bad_request` | 400 | Another request error, such as a form body that does not parse. |
| `not_found` | 404 | No route matches the path (`nanohttp.NewRouter` without a `NotFound` handler). |
| `method_not_allowed` | 405 | The path matches a route only for other methods (`nanohttp.NewRouter` without a `MethodNotAllowed` handler). The `Allow` header lists them. |
| `timeout` | 504 | The request context's deadline expired. |
| `client_closed_request` | 499 | The client went away. |
| `internal` | 500 | Any other error. The response reveals nothing about it. |

The code of each is `http.<name>`.

## Errors from handlers

An error that a handler returns controls its response when it implements `fault.Described` from `core/fault`, as generated error catalogues do. `fault.Detailed` adds the `detail` member and `fault.Coded` adds `code`. Package `fault` owns these interfaces and the type URI base; `http/problem` only renders them. The outermost `Described` error in the chain wins. A `Described` error with a status outside 400–599 or an empty type becomes a 500. `Error()` is never sent to a client.

## Logging

`problem.Write` logs every failed request on slog's default logger with the request context, under `log.KeyError`, `log.KeyProblem`, `log.KeyStatus` and, when the router set one, `log.KeyRoute` from `core/log`. The route names the failed operation; the line's source is `problem.Write`, because a generated server calls it after the handler has returned. `log.ErrorStatusTo` picks the level by the rules of `log.LevelFor`, which owns them. When the request context is done and the error carries no status of its own, `problem.Write` answers 499 or 504 and logs the error joined with the context error, so the line has the level of a cancellation, as the request summary does. A 5xx gives a `request failed` line and an Error summary line from `log.HTTPMiddleware`; [core/log](../core/log/README.md) explains which one to alert on. The boundary imports no other logging library.

## Error hooks

Three hooks receive every error of the generated server. A service that must keep another error shape, such as the legacy `{"errors":[...]}` envelope of `pkg/fault`, replaces them; the problem writers above are only their defaults.

| Hook | Receives | Default |
| --- | --- | --- |
| `HTTPHandlerOptions.RequestErrorHandlerFunc` (or `StrictHTTPServerOptions.RequestErrorHandlerFunc`) | Body decoding and validation errors, parameter validation errors, and parameter binding errors converted to `problem.MissingParameter` (a required parameter is absent) or `problem.InvalidParameter`, which name the parameter and keep the binding error as their cause (`errors.As` finds `RequiredParamError`, `RequiredHeaderError` and the other binding types) | `nanohttp.RequestError` |
| `HTTPHandlerOptions.ResponseErrorHandlerFunc` (or `StrictHTTPServerOptions.ResponseErrorHandlerFunc`) | The error a handler returns, or a failure to write its response | `nanohttp.ResponseError` |
| `nanohttp.RouterOptions.NotFound` and `MethodNotAllowed` | A request that matches no route, or a route only for other methods | a 404 or 405 problem |

`pkg/httputil/errorenvelope` is the writer for a service that keeps the legacy `{"errors":[...]}` envelope: `errorenvelope.New(opts)` gives `RequestError` and `ResponseError` for the two strict hooks, `NotFound`, `MethodNotAllowed` and `Handler(*fault.Error)` for the router, and `PlainRequestError` for anchor's `text/plain` 400. Its package doc lists the mapping of each error.

A request error reaches its hook as the `*problem.Error` that the default writer would render, so a replacement reads the status, code and field errors (`FieldErrors()`, with `Pointer` or `Parameter`) from it instead of parsing text. oapi-codegen's default chi strict server has the same two strict hooks, so one writer serves a service before and after it moves to the generated std-http server.

The framework does not reproduce kin-openapi's message text. A service whose gate flows assert that text updates them to assert the status and code before it adopts generated validation, as the cross-repo rollout rule in the workspace guide requires.

## Routing

`nanohttp.NewRouter(mux, opts)` wraps a `ServeMux` with configurable answers for unmatched requests. ServeMux writes a `text/plain` 404 or 405 itself; through `NewRouter`, a 404 goes to `RouterOptions.NotFound` and a 405 to `RouterOptions.MethodNotAllowed`, with the `Allow` header already set. A matched request goes to the mux unchanged, so `r.Pattern`, the path values and the trailing-slash redirect stay as they are. A request with an unclean path (`//health`, `/a/../health`) goes to the mux for every method and gets its redirect to the clean path, also when the clean path has no route for that method or no route at all; the clean path then gets the answers of `NewRouter`. So every status and redirect is the one of a bare `ServeMux`, except HEAD with `ExplicitHEAD`.

`RouterOptions.ExplicitHEAD` gives chi's answer to HEAD: a GET pattern no longer matches HEAD, so HEAD on a GET-only route is a 405 and `Allow` does not list HEAD for it. ServeMux alone serves HEAD with the GET handler.

Differences from chi that remain, which a service migration PR lists and its golden HTTP suite checks:

- An unclean path, such as `//health`, `/a/../health` or `//nothing`, is redirected to the clean path (307 on Go 1.27) for every method. chi answers 404.
- `Allow` lists only the RFC 9110 methods and PATCH. A route registered for another method is not listed.
- `Allow` lists every method that the path has a route for, as RFC 9110 requires: `GET, PATCH` for `GET` and `PATCH /webhooks/{id}`. chi v5.3.1 lists only one of them (`GET`, sometimes `PATCH`). The status is the same. No exported gate flow asserts `Allow`.
- A path with a percent-encoded unreserved character, such as `/he%61lth`, matches the route of the decoded path (`/health`): ServeMux matches on the decoded path. So `GET /he%61lth` is 200 and `POST /he%61lth` is 405 `Allow: GET`. chi matches on the raw path and answers 404 for every method.

`NewRouter` looks a matched request up twice, once to classify it and once in `ServeMux.ServeHTTP`; a miss costs one lookup per probed method. The router tests pin the `ServeMux.Handler` behaviour that this relies on.

## Accepted strictness differences

`nanohttp.Decode` has no lenient mode. Compared with a server that decodes with `encoding/json` v1 behind the kin-openapi validator, a service that adopts it answers differently in these cases, and lists them in its migration PR:

| Request | Before | With `nanohttp.Decode` |
| --- | --- | --- |
| Body over the limit (1 MiB by default) | read whole, with no limit | 413 `body_too_large`. Set `nanohttp.MaxBodyBytes` per route for large uploads. |
| Body with a non-JSON Content-Type | validator-dependent | 415 `unsupported_media_type` |
| Member name in another case (`"Name"` for `name`) | read into the field | not read; a `required` or `additionalProperties` violation when the schema has one |
| Duplicate member name | last value wins | 400 `malformed_json`, code `duplicate_name` |
| Invalid UTF-8, data after the JSON value | accepted or replaced | 400 `malformed_json` |

Every released client is generated from the spec and sends exact member names and a JSON Content-Type, so a lenient mode would only reopen the ambiguities that ADR 0017 closed.
