package duplicate

import "context"

type Config struct{ DB DBConfig }

type DBConfig struct{ DSN string }

type Store struct{}

func NewStore() *Store      { return &Store{} }
func NewOtherStore() *Store { return &Store{} }

// NewDBConfig provides a type that Config.DB already provides.
func NewDBConfig() DBConfig { return DBConfig{} }

type Server struct{}

func NewServer(*Store, DBConfig) *Server      { return &Server{} }
func (*Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
