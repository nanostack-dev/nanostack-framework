package apierror

import (
	"errors"
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
