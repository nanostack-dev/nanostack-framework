package httpserver

import (
	"fmt"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope"
)

// CodeInvalidCursor is the API error code for a pagination cursor that cannot
// continue the search: malformed, issued for another sort, or sent with an
// offset.
const CodeInvalidCursor = errorenvelope.CodeInvalidCursor

// CodeInvalidSort is the API error code for a sort field the search cannot
// order by.
const CodeInvalidSort = errorenvelope.CodeInvalidSort

// StatusClientClosedRequest is the nginx convention for a request the client
// abandoned before the response was written. net/http has no constant for it.
const StatusClientClosedRequest = errorenvelope.StatusClientClosedRequest

// isServerError reports whether status is a 5xx the boundary should log with
// full diagnostic detail.
func isServerError(status int) bool {
	return status >= http.StatusInternalServerError
}

// APIErrorAdapter converts app-owned legacy errors into framework API errors.
type APIErrorAdapter func(error) (*fault.Error, bool)

// StrictErrorHandlerOptions configures strict-handler request and response errors.
type StrictErrorHandlerOptions struct {
	Logger        zerolog.Logger
	AdaptError    APIErrorAdapter
	IsSSEEndpoint func(*http.Request) bool
}

// StrictErrorHandler writes default API-safe errors for strict OpenAPI handlers.
//
// The status is never inferred from the error message. An error is either a
// framework API error — directly, or via the app-supplied AdaptError adapter —
// in which case its carried status, code and message are returned, or it is an
// unmodelled error, in which case the response is a generic 500 and the error
// is logged with as much detail as possible for diagnosis.
//
// The status and the body come from errorenvelope.Resolve and
// errorenvelope.ResolveRequest, the resolution that errorenvelope.Writer
// uses too. StrictErrorHandler logs through zerolog; a service that logs
// through core/log uses errorenvelope.Writer, whose lines carry the request
// scope of log.HTTPMiddleware (request_id).
type StrictErrorHandler struct {
	logger        zerolog.Logger
	adaptError    APIErrorAdapter
	isSSEEndpoint func(*http.Request) bool
}

// NewStrictErrorHandler creates a reusable strict-handler error writer.
func NewStrictErrorHandler(options StrictErrorHandlerOptions) *StrictErrorHandler {
	return &StrictErrorHandler{
		logger:        options.Logger,
		adaptError:    options.AdaptError,
		isSSEEndpoint: options.IsSSEEndpoint,
	}
}

// HandleRequestError writes request errors (body decoding, validation,
// parameter binding) as a structured 400 response, as
// errorenvelope.ResolveRequest maps them: an http/problem boundary error from
// the nanohttp generated server gets one detail per field error, and any other
// error keeps its text as the message.
func (h *StrictErrorHandler) HandleRequestError(w http.ResponseWriter, r *http.Request, err error) {
	errorenvelope.ResolveRequest(r, err, h.envelopeOptions()).Write(w)
}

// envelopeOptions are the resolution options of h.
func (h *StrictErrorHandler) envelopeOptions() errorenvelope.Options {
	if h == nil {
		return errorenvelope.Options{}
	}
	opts := errorenvelope.Options{IsStream: h.isSSEEndpoint}
	if h.adaptError != nil {
		opts.Adapter = h.adaptError
	}
	return opts
}

// HandleResponseError writes handler errors using the framework default response shape.
func (h *StrictErrorHandler) HandleResponseError(w http.ResponseWriter, r *http.Request, err error) {
	resp := errorenvelope.Resolve(r, err, h.envelopeOptions())
	if resp.Stream {
		h.handleSSEError(w, r, err, resp)
		return
	}

	logger := h.requestLogger(r)

	switch {
	case resp.Abandoned:
		// The client is gone, so nothing reads the response; the 499 exists
		// so the access log records an abandoned request instead of a 500.
		logger.Warn().Err(err).Int("status", resp.Status).
			Msg("Request abandoned by client")
	case resp.Unmodelled:
		// Unmodelled error: never guess a status from the message. The generic
		// 500 keeps internal detail off the wire; this boundary log is the
		// safety net for failures a source layer forgot to log.
		h.logInternalError(logger, err).Int("status", resp.Status).
			Msg("Unhandled error returned by strict handler")
	case isServerError(resp.Status):
		h.logInternalError(logger, err).Int("status", resp.Status).
			Msg("Internal server error")
	default:
		// Modelled client error: expected, low-severity. Info keeps it visible
		// without the noise of warn/error.
		logger.Info().Err(err).Int("status", resp.Status).Msg("Request error")
	}

	resp.Write(w)
}

// logInternalError starts an error-level log event carrying every detail we can
// extract from an unmodelled error: its message, its concrete Go type, and the
// verbose ("%+v") form, which surfaces the wrapped chain and any stack trace.
func (h *StrictErrorHandler) logInternalError(logger zerolog.Logger, err error) *zerolog.Event {
	return logger.Error().
		Err(err).
		Str("error_type", fmt.Sprintf("%T", err)).
		Str("error_detail", fmt.Sprintf("%+v", err))
}

// requestLogger returns the logger the error line should be written to.
//
// It prefers the request-scoped logger on the context, because this handler
// emits the only line carrying the cause of a 5xx — err, error_type,
// error_detail — and that line is useless for support if it cannot be joined to
// the request id the caller was given. Building solely from the injected
// process logger, as this once did, meant a customer reporting a 500 with an
// X-Request-Id could match the access-log line but had to join the cause by
// timestamp.
//
// Falls back to the injected logger where no request-scoped one exists, so the
// handler still works without requestlog.Contextualize.
//
// path and method are added only on that fallback. A request-scoped logger
// already carries both — Contextualize sets them and every Bind republishes
// them — and zerolog does not deduplicate, so adding them again would emit each
// key twice on every error line.
func (h *StrictErrorHandler) requestLogger(r *http.Request) zerolog.Logger {
	logger := zerolog.Nop()
	if h != nil {
		logger = h.logger
	}

	if r != nil {
		if bound := zerolog.Ctx(r.Context()); bound.GetLevel() != zerolog.Disabled {
			return *bound
		}
	}

	ctx := logger.With()
	if r != nil {
		ctx = ctx.Str("path", r.URL.Path).Str("method", r.Method)
	}
	return ctx.Logger()
}

func (h *StrictErrorHandler) handleSSEError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	resp errorenvelope.Response,
) {
	logger := h.requestLogger(r)
	if resp.Abandoned {
		logger.Debug().Msg("SSE stream closed by client")
		return
	}

	if isServerError(resp.Status) {
		h.logInternalError(logger, err).Int("status", resp.Status).Msg("SSE stream error")
	} else {
		logger.Info().Err(err).Int("status", resp.Status).Msg("SSE stream warning")
	}

	if !resp.Write(w) {
		logger.Debug().Msg("SSE stream headers already written, cannot modify response")
	}
}
