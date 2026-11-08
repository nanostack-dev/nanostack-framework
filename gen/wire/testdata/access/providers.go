// Package access generates into internal/gen/wire, a separate package, so
// unexported providers and ambiguous Config fields are errors.
package access

import "context"

type Config struct {
	Region string
	Bucket string // a second string field: string is ambiguous
}

type client struct{}

func newClient() *client { return &client{} }

type Server struct{}

func NewServer(*client, string) *Server       { return &Server{} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
