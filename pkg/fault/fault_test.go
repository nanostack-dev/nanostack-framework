package fault

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestAs(t *testing.T) {
	err := errors.Join(BadRequest("BAD", "bad"))
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

func TestSemanticConstructors(t *testing.T) {
	cases := []struct {
		name   string
		err    *Error
		status int
	}{
		{"BadRequest", BadRequest("X", "x"), http.StatusBadRequest},
		{"Unauthorized", Unauthorized("X", "x"), http.StatusUnauthorized},
		{"Forbidden", Forbidden("X", "x"), http.StatusForbidden},
		{"NotFound", NotFound("X", "x"), http.StatusNotFound},
		{"Conflict", Conflict("X", "x"), http.StatusConflict},
		{"Unprocessable", Unprocessable("X", "x"), http.StatusUnprocessableEntity},
		{"TooManyRequests", TooManyRequests("X", "x"), http.StatusTooManyRequests},
		{"Internal", Internal("X", "x"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.HTTPStatus() != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, tc.err.HTTPStatus())
			}
			if tc.err.Details[0].Code != "X" || tc.err.Details[0].Message != "x" {
				t.Fatalf("unexpected detail %#v", tc.err.Details[0])
			}
		})
	}
}

func TestMetaDoesNotMutateSentinel(t *testing.T) {
	decorated := ErrNotFound.Meta("flow_id", "abc")

	if decorated == ErrNotFound {
		t.Fatal("expected a copy, got the sentinel itself")
	}
	if len(ErrNotFound.Details[0].Metadata) != 0 {
		t.Fatalf("sentinel was mutated: %#v", ErrNotFound.Details[0].Metadata)
	}
	if decorated.Details[0].Metadata["flow_id"] != "abc" {
		t.Fatalf("expected flow_id metadata, got %#v", decorated.Details[0].Metadata)
	}
	if decorated.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("expected status preserved, got %d", decorated.HTTPStatus())
	}
}

func TestMetadataMerge(t *testing.T) {
	err := Conflict("LOCKED", "locked").
		Meta("a", 1).
		Metadata(map[string]any{"b": 2, "c": 3})

	meta := err.Details[0].Metadata
	if meta["a"] != 1 || meta["b"] != 2 || meta["c"] != 3 {
		t.Fatalf("expected merged metadata, got %#v", meta)
	}
}

func TestMsgf(t *testing.T) {
	err := Conflict("FLOW_RUNNING", "").Msgf("flow %s is locked", "abc")
	if err.Details[0].Message != "flow abc is locked" {
		t.Fatalf("unexpected message %q", err.Details[0].Message)
	}
}

func TestWrapPreservesUnwrap(t *testing.T) {
	err := NotFound("FLOW_NOT_FOUND", "flow not found").Wrap(sql.ErrNoRows)

	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expected wrapped error to unwrap to sql.ErrNoRows")
	}
	apiErr, ok := As(err)
	if !ok || apiErr.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("expected wrapped error to remain a 404 api error, got %#v", apiErr)
	}
	if got := apiErr.Details[0].Code; got != "FLOW_NOT_FOUND" {
		t.Fatalf("expected code FLOW_NOT_FOUND, got %s", got)
	}
}

func TestWrapDoesNotLeakSourceInJSON(t *testing.T) {
	err := NotFound("FLOW_NOT_FOUND", "flow not found").Wrap(errors.New("secret db dsn leaked"))
	resp := ToResponse(err)
	if len(resp.Errors) != 1 {
		t.Fatalf("expected one error, got %d", len(resp.Errors))
	}
	if resp.Errors[0].Message != "flow not found" {
		t.Fatalf("expected only the safe message, got %q", resp.Errors[0].Message)
	}
}

func TestIsMatchesByStatusClass(t *testing.T) {
	err := NotFound("FLOW_NOT_FOUND", "flow not found")
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected a 404 error to match the ErrNotFound sentinel")
	}
	if errors.Is(err, ErrConflict) {
		t.Fatal("did not expect a 404 to match a 409 sentinel")
	}
}

func TestFieldPopulatesResponseField(t *testing.T) {
	err := Invalid().
		Field("name", "REQUIRED", "name is required").
		Field("age", "RANGE", "must be positive")

	if err.HTTPStatus() != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", err.HTTPStatus())
	}
	resp := ToResponse(err)
	if len(resp.Errors) != 2 {
		t.Fatalf("expected 2 field errors, got %d", len(resp.Errors))
	}
	if resp.Errors[0].Field == nil || *resp.Errors[0].Field != "name" {
		t.Fatalf("expected field name, got %#v", resp.Errors[0].Field)
	}
	if resp.Errors[1].Field == nil || *resp.Errors[1].Field != "age" {
		t.Fatalf("expected field age, got %#v", resp.Errors[1].Field)
	}
}

func TestErrorMarshalsToContractShape(t *testing.T) {
	err := Conflict("FLOW_RUNNING", "flow already running").Meta("flow_id", "abc")

	direct, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	// A direct marshal of Error must equal the API error contract so the type
	// can back a generated OpenAPI schema via x-go-type with no translation.
	want := `{"errors":[{"code":"FLOW_RUNNING","message":"flow already running","details":{"flow_id":"abc"}}]}`
	if string(direct) != want {
		t.Fatalf("unexpected contract JSON:\n got %s\nwant %s", direct, want)
	}
}

func TestMessageIsClientSafe(t *testing.T) {
	err := NotFound("FLOW_NOT_FOUND", "flow not found").Wrap(errors.New("pq: connection refused"))

	if got := err.Message(); got != "flow not found" {
		t.Fatalf("expected safe message without cause, got %q", got)
	}
	// Error keeps the cause for diagnostics; Message must not.
	if err.Error() == err.Message() {
		t.Fatal("expected Error to include the wrapped cause, Message to omit it")
	}
}

func TestDetailAppends(t *testing.T) {
	err := BadRequest("PRIMARY", "primary").Detail("SECOND", "second")
	if len(err.Details) != 2 {
		t.Fatalf("expected 2 details, got %d", len(err.Details))
	}
	if err.Details[1].Code != "SECOND" {
		t.Fatalf("expected appended detail SECOND, got %s", err.Details[1].Code)
	}
}

func TestDeprecatedConstructorsStillWork(t *testing.T) {
	if NewBadRequest("BAD", "bad").HTTPStatus() != http.StatusBadRequest {
		t.Fatal("NewBadRequest should map to 400")
	}
	withMeta := NewBadRequestWithMetadata("BAD", "bad", map[string]any{"k": "v"})
	if withMeta.Details[0].Metadata["k"] != "v" {
		t.Fatalf("expected metadata preserved, got %#v", withMeta.Details[0].Metadata)
	}
}
