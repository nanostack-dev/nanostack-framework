package billing_test

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/gen/errors/testdata/billing"
)

func TestProblemContract(t *testing.T) {
	id := uuid.MustParse("7f000000-0000-4000-8000-000000000001")
	paidAt := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	var err error = billing.InvoiceAlreadyPaid(id, paidAt)

	d, ok := errors.AsType[fault.Described](err)
	if !ok {
		t.Fatal("generated error does not implement fault.Described")
	}
	if got, want := d.ProblemType(), "https://errors.nanostack.dev/billing/invoice_already_paid"; got != want {
		t.Errorf("ProblemType = %q, want %q", got, want)
	}
	if d.ProblemStatus() != 409 || d.ProblemTitle() != "Invoice is already paid" {
		t.Errorf("status/title = %d %q", d.ProblemStatus(), d.ProblemTitle())
	}
	coded := err.(fault.Coded)
	if coded.ProblemCode() != "billing.invoice_already_paid" {
		t.Errorf("ProblemCode = %q", coded.ProblemCode())
	}
	detailed := err.(fault.Detailed)
	if got, want := detailed.ProblemDetail(), "Invoice "+id.String()+" was paid at 2026-11-02T09:30:00Z."; got != want {
		t.Errorf("ProblemDetail = %q, want %q", got, want)
	}
}

func TestWrapIsAndAsType(t *testing.T) {
	id := uuid.New()
	err := fmt.Errorf("load invoice: %w", billing.InvoiceNotFound(id).Wrap(sql.ErrNoRows))

	if !errors.Is(err, billing.ErrInvoiceNotFound) {
		t.Error("errors.Is does not match the catalogue entry")
	}
	if errors.Is(err, billing.ErrInvoiceAlreadyPaid) {
		t.Error("errors.Is matches another catalogue entry")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Error("errors.Is does not reach the wrapped cause")
	}
	typed, ok := errors.AsType[*billing.InvoiceNotFoundError](err)
	if !ok || typed.ID != id {
		t.Fatalf("errors.AsType = %v, %v", typed, ok)
	}
	if want := "load invoice: billing.invoice_not_found: Invoice not found (id=" + id.String() + "): sql: no rows in result set"; err.Error() != want {
		t.Errorf("Error() = %q\nwant      %q", err.Error(), want)
	}
	if typed.ProblemDetail() != "" {
		t.Errorf("ProblemDetail = %q, want empty: no detail declared", typed.ProblemDetail())
	}
}

func TestWrapDoesNotMutate(t *testing.T) {
	base := billing.PaymentProviderDown()
	wrapped := base.Wrap(errors.New("timeout"))
	if base.Unwrap() != nil {
		t.Error("Wrap mutated the receiver")
	}
	if wrapped.Error() != "billing.provider_unavailable: Payment provider unavailable: timeout" {
		t.Errorf("Error() = %q", wrapped.Error())
	}
}

func TestDetailEscapesPercent(t *testing.T) {
	err := billing.AmountOutOfRange(500, 1, 100)
	if got, want := err.ProblemDetail(), "Amount 500 must be between 1 and 100 (100% cap)."; got != want {
		t.Errorf("ProblemDetail = %q, want %q", got, want)
	}
	if !strings.HasPrefix(err.Error(), "billing.amount_out_of_range: Amount 500") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestKindsListsEveryEntry(t *testing.T) {
	codes := make([]string, len(billing.Kinds))
	for i, k := range billing.Kinds {
		codes[i] = k.ProblemCode()
	}
	want := "billing.invoice_not_found billing.invoice_already_paid billing.provider_unavailable billing.amount_out_of_range"
	if got := strings.Join(codes, " "); got != want {
		t.Errorf("Kinds = %s", got)
	}
	var sentinel error = billing.ErrInvoiceNotFound
	if d, ok := errors.AsType[fault.Described](sentinel); !ok || d.ProblemStatus() != 404 {
		t.Error("a returned sentinel must still render as its problem")
	}
}
