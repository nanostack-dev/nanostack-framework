package missing

import (
	"context"
	"time"
)

type Store interface{ Load() string }

type PGStore struct{}

func NewPGStore() *PGStore    { return &PGStore{} }
func (*PGStore) Load() string { return "" }

type Clock struct{ now func() time.Time }

type Service struct{}

// NewService needs a Store, which nothing binds, and a *Clock, which nothing provides.
func NewService(Store, *Clock) *Service { return &Service{} }

type Server struct{}

func NewServer(*Service) *Server              { return &Server{} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
