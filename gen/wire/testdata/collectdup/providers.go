package collectdup

import "context"

type Route interface{ Pattern() string }

type healthRoute struct{}

func (healthRoute) Pattern() string { return "/healthz" }

func NewHealthRoute() Route { return healthRoute{} }

// NewRoutes provides []Route, which the collection already provides.
func NewRoutes() []Route { return nil }

type Server struct{ routes []Route }

func NewServer(routes []Route) *Server        { return &Server{routes: routes} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
