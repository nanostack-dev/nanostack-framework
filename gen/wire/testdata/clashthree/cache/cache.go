// Package cache has a Ledger too, in a package of another name.
package cache

type Ledger struct{}

func NewLedger() *Ledger { return &Ledger{} }
