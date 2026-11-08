package cycle

import "context"

type A struct{}
type B struct{}
type C struct{}

func NewA(*B) *A { return &A{} }
func NewB(*C) *B { return &B{} }
func NewC(*A) *C { return &C{} }

type Server struct{}

func NewServer(*A) *Server                    { return &Server{} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
