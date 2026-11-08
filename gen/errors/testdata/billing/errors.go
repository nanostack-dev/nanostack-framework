// Package billing is the golden fixture of nano gen errors.
package billing

import (
	"time"

	ids "github.com/google/uuid"
)

// declarations is the billing error catalogue. `nano gen errors` reads it and
// writes errors.gen.go and ERRORS.md.
//
//nano:errors namespace=billing
type declarations interface {
	// No invoice has the requested id.
	//
	//nano:error code=invoice_not_found status=404 title="Invoice not found"
	InvoiceNotFound(id ids.UUID)

	// The invoice was paid already and cannot be paid again.
	//
	//nano:error code=invoice_already_paid status=409 title="Invoice is already paid" detail="Invoice {id} was paid at {paidAt}."
	InvoiceAlreadyPaid(id ids.UUID, paidAt time.Time)

	// The payment provider did not answer in time. Safe to retry.
	//
	//nano:error code=provider_unavailable status=503 title="Payment provider unavailable"
	PaymentProviderDown()

	// The amount is outside what the plan allows; 100% is the ceiling.
	//
	//nano:error code=amount_out_of_range status=422 title="Amount out of range" detail="Amount {amount} must be between {min} and {max} (100% cap)."
	AmountOutOfRange(amount, min, max int64)
}
