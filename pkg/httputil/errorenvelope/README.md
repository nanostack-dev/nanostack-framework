# pkg/httputil/errorenvelope

Writes the legacy error envelope, `{"errors":[{"code","message","metadata"}]}` with `Content-Type: application/json`, for every error of the HTTP boundary (design C3). A service keeps the wire contract of its clients, gate flows and alerts while it moves to the nano HTTP stack and to `core/log`.

```go
envelope := errorenvelope.New(errorenvelope.Options{Logger: logger})
handler := api.NewHTTPHandler(server, api.HTTPHandlerOptions{
	RequestErrorHandlerFunc:  envelope.RequestError,
	ResponseErrorHandlerFunc: envelope.ResponseError,
})
router := nanohttp.NewRouter(mux, nanohttp.RouterOptions{
	NotFound:         envelope.NotFound(),
	MethodNotAllowed: envelope.MethodNotAllowed(),
	ExplicitHEAD:     true,
})
```

`RequestError` and `ResponseError` also fit oapi-codegen's chi strict server.

| Error | Status | Envelope |
| --- | --- | --- |
| `*pkg/fault.Error`, or what `Options.Adapter` returns | its status | its errors, byte-identical to `fault.WriteJSON` |
| `search.ErrInvalidCursor`, `ErrCursorWithOffset`, `ErrUnknownSort` | 400 | `INVALID_CURSOR` (field `pagination.cursor`) or `INVALID_SORT` (field `sort_by`) |
| `core/fault.Described` (generated catalogue error) | its status | `code` = `ProblemCode()` in upper case, `message` = the detail, or the title |
| `http/problem` boundary error (malformed JSON, missing body, invalid parameter, validation) | 400 (a 422 becomes 400) | one detail per field error: `code` = `Options.BoundaryCode` (`BAD_REQUEST`), `message` = `<location>: <detail>`, `metadata` = `{field, rule}` |
| body too large, unsupported media type | 413, 415 | `PAYLOAD_TOO_LARGE`, `UNSUPPORTED_MEDIA_TYPE` |
| other request-hook error | 400 | `BAD_REQUEST` with the error text, as v0.22.0 |
| request abandoned by the client | 499 | no body, no Content-Type |
| other handler error | 500 | `UNEXPECTED_ERROR` / `Unexpected error` |
| any error where `Options.IsStream(r)` is true | its status | `event: error` frame |

`Writer` logs one `request failed` line per error through `core/log`, with the request context, so the line carries `request_id` and the other scope attributes of `log.HTTPMiddleware`. The keys are those of `http/problem.Write`: `error`, `status`, `route`. An unmodelled 500 also carries `error_type` and `error_detail`.

`Options.Log` replaces that line. The Writer calls it once per error, after it writes the response, with a `Failure`: the error, the `Source` (`SourceRequest` for `RequestError`, `SourceResponse` for `ResponseError`, `SourceRouter` for the router handlers), the `Response`, the status the client received, and whether a stream frame was written. A func that does nothing logs nothing. `StrictErrorHandlerLog(logger)` writes the lines of `modules/httpserver.StrictErrorHandler`, which echopoint's and anchor's alerts and contract suites read:

| Failure | Level | Message | Extra keys |
|---|---|---|---|
| `SourceRequest` | none | none | |
| client abandoned the request | Warn | `Request abandoned by client` | `error`, `status` |
| unmodelled error | Error | `Unhandled error returned by strict handler` | `error`, `error_type`, `error_detail`, `status` |
| other 5xx | Error | `Internal server error` | `error`, `error_type`, `error_detail`, `status` |
| other status, the router's included | Info | `Request error` | `error`, `status` |
| stream the client closed | Debug | `SSE stream closed by client` | |
| stream 5xx | Error | `SSE stream error` | `error`, `error_type`, `error_detail`, `status` |
| other stream error | Info | `SSE stream warning` | `error`, `status` |
| stream frame not written | Debug, after the line above | `SSE stream headers already written, cannot modify response` | |

`status` is the status of the error, as `StrictErrorHandler` logged it, not the one a started stream sent.

```go
envelope := errorenvelope.New(errorenvelope.Options{
	IsStream: isSSEEndpoint,
	Log:      errorenvelope.StrictErrorHandlerLog(nil), // nil: slog.Default() at each error
})
```

`PlainRequestError` answers 400 `text/plain` with the parameter-binding error, as anchor answers today.

The framework does not reproduce kin-openapi message text. A service that adopts generated validation updates the gate assertions on that text first. This package is a bridge: it goes away with `pkg/fault`.
