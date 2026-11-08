// Package adapter is a provider that returns one of its dependencies as an
// interface. The App must still run and stop that dependency once.
package adapter

import (
	"context"
	"sync/atomic"
)

// Server counts its lifecycle calls.
type Server struct {
	Runs, Stops atomic.Int64
}

func NewServer() *Server { return &Server{} }

func (s *Server) Run(ctx context.Context) error {
	s.Runs.Add(1)
	<-ctx.Done()
	return nil
}

func (s *Server) Stop(context.Context) error {
	s.Stops.Add(1)
	return nil
}

// Handler is what the routes need. *Server implements it.
type Handler interface {
	Run(ctx context.Context) error
}

// AsHandler adapts a component that is already registered.
func AsHandler(s *Server) Handler { return s }

// Register stands in for mounting routes on the handler.
func Register(Handler) {}
