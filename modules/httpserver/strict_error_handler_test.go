package httpserver

import (
	"bytes"
	"context"
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
		Code     string         `json:"code"`
		Metadata map[string]any `json:"metadata,omitempty"`
		Message  string         `json:"message"`
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
	if body.Errors[0].Metadata["field"] != "name" {
		t.Fatalf("expected field metadata to be preserved, got %#v", body.Errors[0].Metadata)
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
	if !strings.Contains(resp.Body.String(), `{"error":"conflict"}`) {
		t.Fatalf("expected SSE conflict payload, got %s", resp.Body.String())
	}
}

func TestStrictErrorHandlerSSEUsesFaultMessageWithoutLeakingCause(t *testing.T) {
	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{
		Logger:        zerolog.Nop(),
		IsSSEEndpoint: func(*http.Request) bool { return true },
	})
	req := httptest.NewRequest(http.MethodGet, "/events/stream", nil)
	resp := httptest.NewRecorder()

	err := fault.NotFound("FLOW_NOT_FOUND", "flow not found").Wrap(errors.New("pq: connection refused"))
	handler.HandleResponseError(resp, req, err)

	body := resp.Body.String()
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
	if !strings.Contains(body, `{"error":"flow not found"}`) {
		t.Fatalf("expected SSE frame to carry the fault message, got %s", body)
	}
	if strings.Contains(body, "pq: connection refused") {
		t.Fatalf("wrapped cause leaked into SSE frame: %s", body)
	}
}

// The 5xx cause line is the only place err/error_type/error_detail appear, so
// it has to be joinable to the request id the caller was given. Before this,
// requestLogger built solely from the injected process logger and the line
// carried no correlation at all.
func TestStrictErrorHandlerUsesRequestScopedLogger(t *testing.T) {
	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)

	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: base})

	// A real request-scoped logger already carries path and method; the fixture
	// must too, or it cannot catch the handler adding them a second time.
	bound := base.With().
		Str("request_id", "req_fixed").
		Str("org_id", "org_1").
		Str("path", "/flows").
		Str("method", http.MethodGet).
		Logger()
	request := httptest.NewRequest(http.MethodGet, "/flows", nil).WithContext(
		bound.WithContext(context.Background()),
	)

	handler.HandleResponseError(httptest.NewRecorder(), request, errors.New("boom"))

	out := buf.String()
	if !strings.Contains(out, `"request_id":"req_fixed"`) {
		t.Fatalf("error line lost the request id, got %s", out)
	}
	if !strings.Contains(out, `"org_id":"org_1"`) {
		t.Fatalf("error line lost the bound org, got %s", out)
	}
	if !strings.Contains(out, `"path":"/flows"`) {
		t.Fatalf("error line lost the path, got %s", out)
	}
}

// Without Contextualize there is no request-scoped logger; the handler must
// still write the line rather than silently discarding it.
func TestStrictErrorHandlerFallsBackToInjectedLogger(t *testing.T) {
	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel).With().Str("service", "anchor").Logger()

	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: base})
	handler.HandleResponseError(
		httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/flows", nil), errors.New("boom"),
	)

	if out := buf.String(); !strings.Contains(out, `"service":"anchor"`) {
		t.Fatalf("expected the injected logger to be used, got %s", out)
	}
}

// zerolog does not deduplicate keys and json.Unmarshal silently keeps the last,
// so a duplicate is invisible to any test that decodes the line. This asserts
// against the raw output.
func TestStrictErrorHandlerDoesNotDuplicateRouteKeys(t *testing.T) {
	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)

	handler := NewStrictErrorHandler(StrictErrorHandlerOptions{Logger: base})

	bound := base.With().Str("path", "/flows").Str("method", http.MethodGet).Logger()
	request := httptest.NewRequest(http.MethodGet, "/flows", nil).WithContext(
		bound.WithContext(context.Background()),
	)

	handler.HandleResponseError(httptest.NewRecorder(), request, errors.New("boom"))

	out := buf.String()
	for _, key := range []string{`"path"`, `"method"`} {
		if count := strings.Count(out, key); count != 1 {
			t.Fatalf("%s appeared %d times in %s, want exactly 1", key, count, out)
		}
	}
}
