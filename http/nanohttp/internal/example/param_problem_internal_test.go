package example

import (
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

// TestParamProblemKeepsBindingError checks that every parameter-binding error
// stays the cause of its problem. A legacy renderer unwraps it to write the
// generated text ("Query argument limit is required, but not found") and to
// tell a missing query parameter from a missing header.
func TestParamProblemKeepsBindingError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"required query", &RequiredParamError{ParamName: "limit"}, "required"},
		{"required header", &RequiredHeaderError{ParamName: "X-Organization-ID"}, "required"},
		{"invalid format", &InvalidParamFormatError{ParamName: "limit", Err: errors.New("bad")}, "type"},
		{"unmarshaling", &UnmarshalingParamError{ParamName: "filter", Err: errors.New("bad")}, "type"},
		{"too many values", &TooManyValuesForParamError{ParamName: "limit", Count: 2}, "type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := paramProblem(tt.err)
			p, ok := errors.AsType[*problem.Error](got)
			if !ok {
				t.Fatalf("paramProblem(%T) = %T, want *problem.Error", tt.err, got)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("paramProblem(%T) drops the binding error: Unwrap = %v", tt.err, errors.Unwrap(p))
			}
			if code := p.FieldErrors()[0].Code; code != tt.wantCode {
				t.Fatalf("code = %q, want %q", code, tt.wantCode)
			}
		})
	}
	var header *RequiredHeaderError
	if !errors.As(paramProblem(&RequiredHeaderError{ParamName: "X-Organization-ID"}), &header) ||
		header.Error() != "Header parameter X-Organization-ID is required, but not found" {
		t.Fatalf("errors.As does not reach the header error's text")
	}
}
