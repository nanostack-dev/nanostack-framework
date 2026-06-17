package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/pkg/apierror"
	"github.com/rs/zerolog"
)

const internalServerErrorThreshold = 500

// APIErrorAdapter converts app-owned legacy errors into framework API errors.
type APIErrorAdapter func(error) (*apierror.Error, bool)

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
	apierror.WriteJSON(w, apierror.NewBadRequest("BAD_REQUEST", message))
}

// HandleResponseError writes handler errors using the framework default response shape.
func (h *StrictErrorHandler) HandleResponseError(w http.ResponseWriter, r *http.Request, err error) {
	if h != nil && h.isSSEEndpoint != nil && h.isSSEEndpoint(r) {
		h.handleSSEError(w, r, err)
		return
	}

	logger := h.requestLogger(r)
	classification := h.classify(err)

	switch classification.Kind {
	case apierror.KindHandledAPI:
		status := classification.Status
		if status >= internalServerErrorThreshold {
			h.logInternalError(logger, err).Int("status", status).Msg("Internal server error")
		} else {
			logger.Debug().Err(err).Int("status", status).Msg("Request error")
		}
		apierror.WriteJSON(w, classification.APIError)
		return
	case apierror.KindReportedUnexpected:
		apierror.WriteJSON(w, apierror.ErrUnexpected)
		return
	}

	// Unmodelled error: never guess a status from the message. Respond with a
	// generic, API-safe 500 and log the error with full detail so the
	// unexpected failure can be diagnosed.
	h.logInternalError(logger, err).
		Int("status", http.StatusInternalServerError).
		Msg("Unhandled error returned by strict handler")
	apierror.WriteJSON(w, apierror.ErrUnexpected)
}

func (h *StrictErrorHandler) classify(err error) apierror.Classification {
	classification := apierror.Classify(err)
	if classification.Kind != apierror.KindUnexpected || h == nil || h.adaptError == nil {
		return classification
	}

	apiErr, ok := h.adaptError(err)
	if !ok || apiErr == nil {
		return classification
	}

	return apierror.Classification{
		Kind:     apierror.KindHandledAPI,
		Err:      err,
		APIError: apiErr,
		Status:   apiErr.HTTPStatus(),
	}
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

	status := http.StatusInternalServerError
	classification := h.classify(err)
	if classification.Kind == apierror.KindHandledAPI {
		status = classification.Status
	}

	if classification.Kind == apierror.KindReportedUnexpected {
		status = http.StatusInternalServerError
	} else if status >= internalServerErrorThreshold {
		h.logInternalError(logger, err).Int("status", status).Msg("SSE stream error")
	} else {
		logger.Debug().Err(err).Int("status", status).Msg("SSE stream warning")
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
	_, _ = w.Write([]byte(`event: error` + "\n" + `data: {"error":"` + messageFromStatus(status) + `"}` + "\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
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
