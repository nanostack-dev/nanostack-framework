package fault_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
)

func TestKindIsADescribedSentinel(t *testing.T) {
	k := fault.NewKind(
		"billing.invoice_not_found",
		404,
		"Invoice not found",
		fault.DefaultTypeBase+"/billing/invoice_not_found",
	)
	err := fmt.Errorf("lookup: %w", k)

	if !errors.Is(err, k) {
		t.Error("errors.Is must match a wrapped Kind")
	}
	d, ok := errors.AsType[fault.Coded](err)
	if !ok {
		t.Fatal("Kind must implement fault.Coded")
	}
	if d.ProblemType() != "https://errors.nanostack.dev/billing/invoice_not_found" || d.ProblemStatus() != 404 ||
		d.ProblemTitle() != "Invoice not found" || d.ProblemCode() != "billing.invoice_not_found" {
		t.Errorf("problem fields = %q %d %q %q", d.ProblemType(), d.ProblemStatus(), d.ProblemTitle(), d.ProblemCode())
	}
	if k.Error() != "billing.invoice_not_found: Invoice not found" {
		t.Errorf("Error() = %q", k.Error())
	}
}

type described struct {
	status int
	typ    string
}

func (d described) Error() string        { return "x" }
func (d described) ProblemType() string  { return d.typ }
func (d described) ProblemStatus() int   { return d.status }
func (d described) ProblemTitle() string { return "" }

func TestValid(t *testing.T) {
	const typ = fault.DefaultTypeBase + "/x/y"
	for _, tc := range []struct {
		d    described
		want bool
	}{
		{described{400, typ}, true},
		{described{599, typ}, true},
		{described{404, ""}, false},
		{described{399, typ}, false},
		{described{600, typ}, false},
		{described{0, typ}, false},
	} {
		if got := fault.Valid(tc.d); got != tc.want {
			t.Errorf("Valid(%d, %q) = %v, want %v", tc.d.status, tc.d.typ, got, tc.want)
		}
	}
}
