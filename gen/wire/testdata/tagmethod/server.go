package tagmethod

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/tagmethod/api"
)

type Server struct{ h *api.Handlers }

// NewServer serves the Handlers.
func NewServer(h *api.Handlers) *Server       { return &Server{h: h} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
