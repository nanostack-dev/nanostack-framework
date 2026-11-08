// Package greeter is a feature package: a service and the port it needs.
package greeter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// Store is the greeter's port. Adapters live elsewhere.
type Store interface {
	Put(ctx context.Context, name string) error
	Names(ctx context.Context) ([]string, error)
}

type Config struct {
	Greeting string
}

// Service greets known names. It is a Starter (it loads the names once) and
// a HealthChecker (it is healthy once loaded).
type Service struct {
	cfg   Config
	store Store
	log   *slog.Logger

	mu    sync.RWMutex
	known map[string]bool
}

func NewService(cfg Config, store Store, log *slog.Logger) (*Service, error) {
	if cfg.Greeting == "" {
		return nil, errors.New("greeter: empty greeting")
	}
	return &Service{cfg: cfg, store: store, log: log}, nil
}

func (s *Service) Start(ctx context.Context) error {
	names, err := s.store.Names(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	s.mu.Lock()
	s.known = known
	s.mu.Unlock()
	s.log.Info("greeter loaded", "names", len(names))
	return nil
}

func (s *Service) CheckHealth(context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.known == nil {
		return errors.New("greeter: names not loaded")
	}
	return nil
}

func (s *Service) Greet(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.known[name] {
		return "", fmt.Errorf("greeter: unknown name %q", name)
	}
	return s.cfg.Greeting + ", " + name, nil
}
