package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/rs/zerolog"
)

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

// HandleRequestError writes malformed request errors as a structured 400 response.
func (h *StrictErrorHandler) HandleRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	message := http.StatusText(http.StatusBadRequest)
	if err != nil {
		message = err.Error()
	}
	fault.WriteJSON(w, fault.BadRequest("BAD_REQUEST", message))
}

// HandleResponseError writes handler errors using the framework default response shape.
func (h *StrictErrorHandler) HandleResponseError(w http.ResponseWriter, r *http.Request, err error) {
	if h != nil && h.isSSEEndpoint != nil && h.isSSEEndpoint(r) {
		h.handleSSEError(w, r, err)
		return
	}

	logger := h.requestLogger(r)
	apiErr, modelled := h.resolve(err)

	switch {
	case !modelled:
		// Unmodelled error: never guess a status from the message. The generic
		// 500 keeps internal detail off the wire; this boundary log is the
		// safety net for failures a source layer forgot to log.
		h.logInternalError(logger, err).Int("status", apiErr.HTTPStatus()).
			Msg("Unhandled error returned by strict handler")
	case isServerError(apiErr.HTTPStatus()):
		h.logInternalError(logger, err).Int("status", apiErr.HTTPStatus()).
			Msg("Internal server error")
	default:
		// Modelled client error: expected, low-severity. Info keeps it visible
		// without the noise of warn/error.
		logger.Info().Err(err).Int("status", apiErr.HTTPStatus()).Msg("Request error")
	}

	fault.WriteJSON(w, apiErr)
}

// resolve maps err to the fault.Error to write and reports whether err was a
// modelled fault (true) or an unmodelled failure (false). Unmodelled failures
// collapse to a generic 500 so internal detail never reaches the client.
func (h *StrictErrorHandler) resolve(err error) (*fault.Error, bool) {
	if resolved, ok := h.faultFrom(err); ok {
		return resolved, true
	}
	return fault.ErrUnexpected, false
}

// faultFrom recovers a framework error from err, directly or via the
// app-supplied adapter for legacy error types.
func (h *StrictErrorHandler) faultFrom(err error) (*fault.Error, bool) {
	if apiErr, ok := fault.As(err); ok {
		return apiErr, true
	}
	if h != nil && h.adaptError != nil {
		return h.adaptError(err)
	}
	return nil, false
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

func (h *StrictErrorHandler) requestLogger(r *http.Request) zerolog.Logger {
	logger := zerolog.Nop()
	if h != nil {
		logger = h.logger
	}
	ctx := logger.With()
	if r != nil {
		ctx = ctx.Str("path", r.URL.Path).Str("method", r.Method)
	}
	return ctx.Logger()
}

func (h *StrictErrorHandler) handleSSEError(w http.ResponseWriter, r *http.Request, err error) {
	logger := h.requestLogger(r)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		logger.Debug().Msg("SSE stream closed by client")
		return
	}

	apiErr, _ := h.resolve(err)
	status := apiErr.HTTPStatus()

	if isServerError(status) {
		h.logInternalError(logger, err).Int("status", status).Msg("SSE stream error")
	} else {
		logger.Info().Err(err).Int("status", status).Msg("SSE stream warning")
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" && contentType != "" {
		logger.Debug().Msg("SSE stream headers already written, cannot modify response")
		return
	}

	if contentType == "" {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
	}

	w.WriteHeader(status)
	writeSSEError(w, messageFromStatus(status))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// writeSSEError emits a single API-safe SSE error event. The payload is
// JSON-encoded so a message containing quotes can never break the frame.
func writeSSEError(w http.ResponseWriter, message string) {
	payload, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: message})
	if err != nil {
		payload = []byte(`{"error":"Internal server error"}`)
	}
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
}

func messageFromStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "Bad request"
	case http.StatusUnauthorized:
		return "Unauthorized"
	case http.StatusForbidden:
		return "Forbidden"
	case http.StatusNotFound:
		return "Resource not found"
	case http.StatusConflict:
		return "Conflict"
	default:
		return "Internal server error"
	}
}
