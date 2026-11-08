// Package store keeps the eu billing ledger.
package store

type Ledger struct{}

func NewLedger() *Ledger { return &Ledger{} }
