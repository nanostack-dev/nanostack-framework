// Package clashtwo provides two types of one name from two packages of one
// name. Their fields and variables are qualified by their import paths.
package clashtwo

import (
	"context"

	billing "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashtwo/billing/repository"
	invoice "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashtwo/invoice/repository"
)

// Server takes both repositories.
type Server struct {
	Billing *billing.Repository
	Invoice *invoice.Repository
}

func NewServer(b *billing.Repository, i *invoice.Repository) *Server {
	return &Server{Billing: b, Invoice: i}
}

func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
