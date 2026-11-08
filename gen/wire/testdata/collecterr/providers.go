package collecterr

import "context"

type Route interface{ Pattern() string }

type healthRoute struct{}

func (healthRoute) Pattern() string { return "/healthz" }

func NewHealthRoute() Route { return healthRoute{} }

// NotARoute lacks Pattern, so it cannot join the collection.
type NotARoute struct{}

func NewNotARoute() *NotARoute { return &NotARoute{} }

type Server struct{ routes []Route }

func NewServer(routes []Route) *Server        { return &Server{routes: routes} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
