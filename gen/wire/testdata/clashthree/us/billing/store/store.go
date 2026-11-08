// Package store keeps the us billing ledger.
package store

type Ledger struct{}

func NewLedger() *Ledger { return &Ledger{} }
