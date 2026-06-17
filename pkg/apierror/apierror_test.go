package apierror

import (
	"errors"
	"net/http"
	"testing"
)

func TestAs(t *testing.T) {
	err := errors.Join(NewBadRequest("BAD", "bad"))
	apiErr, ok := As(err)
	if !ok {
		t.Fatal("expected api error")
	}
	if apiErr.HTTPStatus() != 400 {
		t.Fatalf("expected 400, got %d", apiErr.HTTPStatus())
	}
}

func TestNewWithDetails(t *testing.T) {
	err := NewWithDetails([]Detail{{
		Code:     "INVALID_INPUT",
		Message:  "invalid input",
		Metadata: map[string]any{"field": "name"},
	}}, 422)

	if err.StatusCode() != 422 {
		t.Fatalf("expected 422, got %d", err.StatusCode())
	}

	resp := ToResponse(err)
	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}
	if resp.Errors[0].Code != "INVALID_INPUT" {
		t.Fatalf("expected code INVALID_INPUT, got %s", resp.Errors[0].Code)
	}
	if resp.Errors[0].Details["field"] != "name" {
		t.Fatalf("expected details.field=name, got %#v", resp.Errors[0].Details)
	}
}

func TestClassifyHandledAPIError(t *testing.T) {
	err := NewWithStatus("CONFLICT", "conflict", http.StatusConflict)

	classification := Classify(err)

	if !classification.Handled() {
		t.Fatalf("expected handled classification, got %s", classification.Kind)
	}
	if classification.ReportedUnexpected() || classification.Unexpected() {
		t.Fatalf("expected only handled classification, got %s", classification.Kind)
	}
	if classification.APIError != err {
		t.Fatal("expected original API error")
	}
	if classification.Status != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, classification.Status)
	}
}

func TestClassifyReportedUnexpected(t *testing.T) {
	sourceErr := errors.New("database unavailable")
	err := MarkReportedUnexpected(sourceErr)

	classification := Classify(err)

	if !classification.ReportedUnexpected() {
		t.Fatalf("expected reported unexpected classification, got %s", classification.Kind)
	}
	if classification.Handled() || classification.Unexpected() {
		t.Fatalf("expected only reported unexpected classification, got %s", classification.Kind)
	}
	if classification.APIError != ErrUnexpected {
		t.Fatal("expected generic unexpected API error")
	}
	if classification.Status != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, classification.Status)
	}
	if !errors.Is(err, sourceErr) {
		t.Fatal("expected marked error to unwrap source error")
	}
	if !IsReportedUnexpected(err) {
		t.Fatal("expected error to be marked reported unexpected")
	}
}

func TestClassifyUnexpected(t *testing.T) {
	err := errors.New("boom")

	classification := Classify(err)

	if !classification.Unexpected() {
		t.Fatalf("expected unexpected classification, got %s", classification.Kind)
	}
	if classification.Handled() || classification.ReportedUnexpected() {
		t.Fatalf("expected only unexpected classification, got %s", classification.Kind)
	}
	if classification.APIError != ErrUnexpected {
		t.Fatal("expected generic unexpected API error")
	}
}

func TestMarkReportedUnexpectedLeavesAPIErrorHandled(t *testing.T) {
	err := NewBadRequest("BAD", "bad")

	marked := MarkReportedUnexpected(err)
	classification := Classify(marked)

	if marked != err {
		t.Fatal("expected API error to be returned unchanged")
	}
	if !classification.Handled() {
		t.Fatalf("expected handled classification, got %s", classification.Kind)
	}
	if IsReportedUnexpected(marked) {
		t.Fatal("expected API error not to be marked reported unexpected")
	}
}
