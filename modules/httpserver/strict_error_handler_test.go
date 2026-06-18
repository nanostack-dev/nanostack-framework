package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/rs/zerolog"
)

type errorResponse struct {
	Errors []struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details,omitempty"`
		Message string         `json:"message"`
	} `json:"errors"`
}

type legacyError struct{}

func (legacyError) Error() string { return "legacy error" }

func TestStrictErrorHandlerHandleRequestError(t *testing.T) {
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: zerolog.Nop()})
	req := httptest.NewRequest(http.MethodPost, "/collections", nil)
	resp := httptest.NewRecorder()

	handler.HandleRequestError(resp, req, errors.New("invalid body"))

	var body errorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
	if len(body.Errors) != 1 {
		t.Fatalf("expected one error, got %d", len(body.Errors))
	}
	if body.Errors[0].Code != "BAD_REQUEST" {
		t.Fatalf("expected BAD_REQUEST code, got %s", body.Errors[0].Code)
	}
	if body.Errors[0].Message != "invalid body" {
		t.Fatalf("expected invalid body message, got %s", body.Errors[0].Message)
	}
}

func TestStrictErrorHandlerHandleResponseErrorWithFrameworkError(t *testing.T) {
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: zerolog.Nop()})
	req := httptest.NewRequest(http.MethodGet, "/flows", nil)
	resp := httptest.NewRecorder()

	handler.HandleResponseError(resp, req, fault.NewWithStatus("CONFLICT", "conflict", http.StatusConflict))

	var body errorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, resp.Code)
	}
	if len(body.Errors) != 1 {
		t.Fatalf("expected one error, got %d", len(body.Errors))
	}
	if body.Errors[0].Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT code, got %s", body.Errors[0].Code)
	}
	if body.Errors[0].Message != "conflict" {
		t.Fatalf("expected conflict message, got %s", body.Errors[0].Message)
	}
}

func TestStrictErrorHandlerLogsHandledClientErrorAtInfo(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.DebugLevel)
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: logger})
	req := httptest.NewRequest(http.MethodGet, "/flows", nil)
	resp := httptest.NewRecorder()

	handler.HandleResponseError(resp, req, fault.Conflict("CONFLICT", "conflict"))

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, resp.Code)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, `"level":"info"`) {
		t.Fatalf("expected info log for handled client error, got %s", logOutput)
	}
	if strings.Contains(logOutput, `"level":"error"`) {
		t.Fatalf("expected no error log for handled client error, got %s", logOutput)
	}
}

func TestStrictErrorHandlerReturnsWrappedAPIErrorStatus(t *testing.T) {
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: zerolog.Nop()})
	req := httptest.NewRequest(http.MethodGet, "/flows", nil)
	resp := httptest.NewRecorder()

	wrapped := fault.NotFound("FLOW_NOT_FOUND", "flow not found").Wrap(errors.New("no rows"))
	handler.HandleResponseError(resp, req, wrapped)

	var body errorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != "FLOW_NOT_FOUND" {
		t.Fatalf("expected FLOW_NOT_FOUND, got %#v", body.Errors)
	}
	if strings.Contains(resp.Body.String(), "no rows") {
		t.Fatalf("wrapped cause leaked into response: %s", resp.Body.String())
	}
}

func TestStrictErrorHandlerLogsUnexpectedErrorAtError(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.DebugLevel)
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: logger})
	req := httptest.NewRequest(http.MethodGet, "/flows", nil)
	resp := httptest.NewRecorder()

	handler.HandleResponseError(resp, req, errors.New("database unavailable"))

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, resp.Code)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, `"level":"error"`) {
		t.Fatalf("expected error log for unexpected error, got %s", logOutput)
	}
	if !strings.Contains(logOutput, "Unhandled error returned by strict handler") {
		t.Fatalf("expected strict handler error message, got %s", logOutput)
	}
}

func TestStrictErrorHandlerHandleResponseErrorWithAdapter(t *testing.T) {
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{
		Logger: zerolog.Nop(),
		AdaptError: func(err error) (*fault.Error, bool) {
			var target *legacyError
			if !errors.As(err, &target) {
				return nil, false
			}
			return fault.BadRequest("INVALID_INPUT", "invalid input").
				Metadata(map[string]any{"field": "name"}), true
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/flows", nil)
	resp := httptest.NewRecorder()

	handler.HandleResponseError(resp, req, &legacyError{})

	var body errorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
	if len(body.Errors) != 1 {
		t.Fatalf("expected one error, got %d", len(body.Errors))
	}
	if body.Errors[0].Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT code, got %s", body.Errors[0].Code)
	}
	if body.Errors[0].Details["field"] != "name" {
		t.Fatalf("expected field metadata to be preserved, got %#v", body.Errors[0].Details)
	}
}

func TestStrictErrorHandlerHandleResponseErrorWithSSEEndpoint(t *testing.T) {
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{
		Logger: zerolog.Nop(),
		AdaptError: func(err error) (*fault.Error, bool) {
			var target *legacyError
			if !errors.As(err, &target) {
				return nil, false
			}
			return fault.NewWithStatus("CONFLICT", "conflict", http.StatusConflict), true
		},
		IsSSEEndpoint: func(*http.Request) bool { return true },
	})
	req := httptest.NewRequest(http.MethodGet, "/events/stream", nil)
	resp := httptest.NewRecorder()

	handler.HandleResponseError(resp, req, &legacyError{})

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, resp.Code)
	}
	if got := resp.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %s", got)
	}
	if !strings.Contains(resp.Body.String(), `event: error`) {
		t.Fatalf("expected SSE error event, got %s", resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `{"error":"Conflict"}`) {
		t.Fatalf("expected SSE conflict payload, got %s", resp.Body.String())
	}
}
