// Package signatures exercises every provider shape nano gen wire accepts.
package signatures

import (
	"context"
	"errors"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/signatures/lib"
)

type Pool struct{ dsn string }

func NewPool(_ context.Context, cfg DBConfig) (*Pool, error) {
	if cfg.DSN == "" {
		return nil, errors.New("empty DSN")
	}
	return &Pool{dsn: cfg.DSN}, nil
}
func (p *Pool) Stop(context.Context) error        { return nil }
func (p *Pool) CheckHealth(context.Context) error { return nil }

type File struct{ name string }

func OpenFile(name string) (*File, func(), error) { return &File{name: name}, func() {}, nil }

type User struct{}

type Repo[T any] struct{ pool *Pool }

func NewRepo[T any](p *Pool) *Repo[T] { return &Repo[T]{pool: p} }

type Store interface{ Get() string }

type MemStore struct{ file *File }

func NewMemStore(_ *Repo[User], f *File) *MemStore { return &MemStore{file: f} }
func (m *MemStore) Get() string                    { return m.file.name }

type Cache struct{ store Store }

func NewCache(s Store) *Cache                { return &Cache{store: s} }
func (c *Cache) Start(context.Context) error { return nil }
func (c *Cache) Stop(context.Context) error  { return nil }

type Settings struct{ Verbose bool }

func NewSettings() Settings { return Settings{} }

type Server struct {
	cache    *Cache
	clock    lib.Clock
	settings Settings
	app      *nano.App
}

func NewServer(c *Cache, clock lib.Clock, s Settings, app *nano.App) *Server {
	return &Server{cache: c, clock: clock, settings: s, app: app}
}

func (s *Server) Run(ctx context.Context) error { <-ctx.Done(); return nil }

func Seed(context.Context, Store) error { return nil }
