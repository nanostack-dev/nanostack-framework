package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/apierror"
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

	handler.HandleResponseError(resp, req, apierror.NewWithStatus("CONFLICT", "conflict", http.StatusConflict))

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

func TestStrictErrorHandlerHandleResponseErrorWithAdapter(t *testing.T) {
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{
		Logger: zerolog.Nop(),
		AdaptError: func(err error) (*apierror.Error, bool) {
			var target *legacyError
			if !errors.As(err, &target) {
				return nil, false
			}
			return apierror.New(
				"INVALID_INPUT",
				"invalid input",
				map[string]any{"field": "name"},
				http.StatusBadRequest,
			), true
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
		AdaptError: func(err error) (*apierror.Error, bool) {
			var target *legacyError
			if !errors.As(err, &target) {
				return nil, false
			}
			return apierror.NewWithStatus("CONFLICT", "conflict", http.StatusConflict), true
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
