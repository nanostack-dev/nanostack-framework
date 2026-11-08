// Package store keeps the order ledger. Its path is shorter than the billing
// stores' paths.
package store

type Ledger struct{}

func NewLedger() *Ledger { return &Ledger{} }
