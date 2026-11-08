package unused

import "context"

type Server struct{}

func NewServer() *Server                      { return &Server{} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }

// Metrics is provided but nothing that runs needs it.
type Metrics struct{}

func NewMetrics() *Metrics { return &Metrics{} }

type Store interface{ Get() string }

type MemStore struct{}

func NewMemStore() *MemStore  { return &MemStore{} }
func (*MemStore) Get() string { return "" }

// NewStoreFromServer is a member of a collection nothing takes.
func NewStoreFromServer(*Server) Store { return &MemStore{} }
