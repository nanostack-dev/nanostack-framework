package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"

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

	if apiErr, ok := h.apiErrorFrom(err); ok {
		apierror.WriteJSON(w, apiErr)
		return
	}

	status := h.statusFromError(err)
	code := codeFromStatus(status)
	message := messageFromStatus(status)

	logger := h.requestLogger(r)
	if status >= internalServerErrorThreshold {
		logger.Error().Err(err).Int("status", status).Msg("Internal server error")
	} else {
		logger.Warn().Err(err).Int("status", status).Msg("Request error")
	}

	apierror.WriteJSON(w, apierror.NewWithStatus(code, message, status))
}

func (h *StrictErrorHandler) apiErrorFrom(err error) (*apierror.Error, bool) {
	if apiErr, ok := apierror.As(err); ok {
		return apiErr, true
	}
	if h != nil && h.adaptError != nil {
		return h.adaptError(err)
	}
	return nil, false
}

func (h *StrictErrorHandler) statusFromError(err error) int {
	if apiErr, ok := h.apiErrorFrom(err); ok {
		return apiErr.HTTPStatus()
	}
	if err == nil {
		return http.StatusInternalServerError
	}

	var httpErr interface{ StatusCode() int }
	if errors.As(err, &httpErr) {
		if sc := httpErr.StatusCode(); sc >= http.StatusBadRequest && sc <= 599 {
			return sc
		}
	}

	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "not found"):
		return http.StatusNotFound
	case strings.Contains(s, "unauthorized"):
		return http.StatusUnauthorized
	case strings.Contains(s, "forbidden"):
		return http.StatusForbidden
	case strings.Contains(s, "conflict"):
		return http.StatusConflict
	case strings.Contains(s, "invalid"), strings.Contains(s, "bad request"), strings.Contains(s, "validation"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
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

	status := h.statusFromError(err)
	if status >= internalServerErrorThreshold {
		logger.Error().Err(err).Int("status", status).Msg("SSE stream error")
	} else {
		logger.Warn().Err(err).Int("status", status).Msg("SSE stream warning")
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

func codeFromStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	default:
		return "INTERNAL_ERROR"
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
