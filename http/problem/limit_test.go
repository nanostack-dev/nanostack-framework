package problem_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

func TestValidationListsAtMostMaxErrors(t *testing.T) {
	var errs validate.Errors
	for i := range 5 * validate.MaxErrors {
		errs.Add(validate.Root.Field("headers").Index(i), validate.CodeRequired, "is required")
	}
	got, _ := write(t, problem.Validation(errs))
	if len(got.Errors) != validate.MaxErrors {
		t.Fatalf("errors = %d, want %d", len(got.Errors), validate.MaxErrors)
	}
	want := "More than 100 violations in the request body; the first 100 are listed."
	if got.Detail != want {
		t.Fatalf("detail = %q, want %q", got.Detail, want)
	}

	got, _ = write(t, problem.InvalidParameters(errs))
	if len(got.Errors) != validate.MaxErrors ||
		got.Detail != "More than 100 violations in the request parameters; the first 100 are listed." {
		t.Fatalf("parameters problem = %d errors, detail %q", len(got.Errors), got.Detail)
	}
}
