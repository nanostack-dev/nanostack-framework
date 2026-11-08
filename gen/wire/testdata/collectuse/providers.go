package collectuse

import "context"

type Route interface{ Pattern() string }

type healthRoute struct{}

func (healthRoute) Pattern() string { return "/healthz" }

func NewHealthRoute() Route { return healthRoute{} }

// Server takes one Route, but routes are collected: it must take []Route.
type Server struct{ route Route }

func NewServer(route Route) *Server           { return &Server{route: route} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
