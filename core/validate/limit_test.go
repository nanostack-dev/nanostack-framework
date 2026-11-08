package validate_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
)

func TestErrorsKeepsOneViolationPastTheLimit(t *testing.T) {
	var errs validate.Errors
	for i := range 10 * validate.MaxErrors {
		errs.Add(validate.Root.Index(i), validate.CodeRequired, "is required")
	}
	if len(errs) != validate.MaxErrors+1 {
		t.Fatalf("len = %d, want %d", len(errs), validate.MaxErrors+1)
	}
	if !errs.Full() || !errs.Truncated() {
		t.Fatal("an accumulator past the limit must be Full and Truncated")
	}
	if got := errs.Listed(); len(got) != validate.MaxErrors || got[0].Pointer != "/0" {
		t.Fatalf("Listed = %d violations starting at %q", len(got), got[0].Pointer)
	}
}

func TestErrorsAtTheLimitIsNotTruncated(t *testing.T) {
	var errs validate.Errors
	for i := range validate.MaxErrors {
		errs.Add(validate.Root.Index(i), validate.CodeRequired, "is required")
	}
	if errs.Full() || errs.Truncated() || len(errs.Listed()) != validate.MaxErrors {
		t.Fatalf("exactly MaxErrors violations: Full=%v Truncated=%v", errs.Full(), errs.Truncated())
	}
}

func TestAppendStopsAtTheLimit(t *testing.T) {
	var child validate.Errors
	for i := range validate.MaxErrors {
		child.Add(validate.Root.Index(i), validate.CodeRequired, "is required")
	}
	var errs validate.Errors
	errs.Add(validate.Root, validate.CodeType, "must be an object")
	errs.Append(child)
	errs.Append(child)
	if len(errs) != validate.MaxErrors+1 || !errs.Truncated() {
		t.Fatalf("len = %d, want %d", len(errs), validate.MaxErrors+1)
	}
}

func TestOnlyMembersStopsAtTheLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for i := range 5000 {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"m` + strconv.Itoa(i) + `":0`)
	}
	b.WriteString("}")
	var errs validate.Errors
	errs.OnlyMembers(validate.RootOf([]byte(b.String())), "name")
	if len(errs) != validate.MaxErrors+1 {
		t.Fatalf("len = %d, want %d", len(errs), validate.MaxErrors+1)
	}
}

func TestErrorStringIsBounded(t *testing.T) {
	var errs validate.Errors
	for i := range 10 * validate.MaxErrors {
		errs.Add(validate.Root.Index(i), validate.CodeRequired, "is required")
	}
	msg := errs.Error()
	if n := strings.Count(msg, "(required)"); n != validate.MaxErrors {
		t.Fatalf("Error lists %d violations, want %d", n, validate.MaxErrors)
	}
	if !strings.HasSuffix(msg, "; and more") {
		t.Fatalf("Error = ...%q, want it to say more violations exist", msg[len(msg)-30:])
	}
}
