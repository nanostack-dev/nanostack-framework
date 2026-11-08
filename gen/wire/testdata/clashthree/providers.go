// Package clashthree provides one type name from three packages of one name,
// at two depths, and from a package of another name. The first three are
// qualified by their import paths, the last by its package name.
package clashthree

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/cache"
	eu "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/eu/billing/store"
	orders "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/orders/store"
	us "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/us/billing/store"
)

// Server takes every ledger.
type Server struct {
	EU     *eu.Ledger
	US     *us.Ledger
	Orders *orders.Ledger
	Cache  *cache.Ledger
}

func NewServer(e *eu.Ledger, u *us.Ledger, o *orders.Ledger, c *cache.Ledger) *Server {
	return &Server{EU: e, US: u, Orders: o, Cache: c}
}

func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
