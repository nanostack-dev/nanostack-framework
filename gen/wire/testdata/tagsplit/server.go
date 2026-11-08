package tagsplit

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/tagsplit/api"
)

type Server struct{ h *api.Handlers }

// NewServer serves the Handlers. No provider returns BooksServer: the
// books handler lacks GetBook. None returns AuthorsServer.
func NewServer(h *api.Handlers) *Server       { return &Server{h: h} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
