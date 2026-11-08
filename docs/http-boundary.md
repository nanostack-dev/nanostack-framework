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
| `timeout` | 504 | The request context's deadline expired. |
| `client_closed_request` | 499 | The client went away. |
| `internal` | 500 | Any other error. The response reveals nothing about it. |

The code of each is `http.<name>`.

## Errors from handlers

An error that a handler returns controls its response when it implements `fault.Described` from `core/fault`, as generated error catalogues do. `fault.Detailed` adds the `detail` member and `fault.Coded` adds `code`. Package `fault` owns these interfaces and the type URI base; `http/problem` only renders them. The outermost `Described` error in the chain wins. A `Described` error with a status outside 400–599 or an empty type becomes a 500. `Error()` is never sent to a client.

## Logging

`problem.Write` logs every failed request on slog's default logger with the request context, under `log.KeyError`, `log.KeyProblem`, `log.KeyStatus` and, when the router set one, `log.KeyRoute` from `core/log`. The route names the failed operation; the line's source is `problem.Write`, because a generated server calls it after the handler has returned. `log.ErrorStatusTo` picks the level by the rules of `log.LevelFor`, which owns them. When the request context is done and the error carries no status of its own, `problem.Write` answers 499 or 504 and logs the error joined with the context error, so the line has the level of a cancellation, as the request summary does. A 5xx gives a `request failed` line and an Error summary line from `log.HTTPMiddleware`; [core/log](../core/log/README.md) explains which one to alert on. The boundary imports no other logging library.
