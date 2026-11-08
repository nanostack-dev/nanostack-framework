// Package store is an adapter for greeter.Store, shipped as a module.
package store

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// Module is what a manifest includes. NewMemory runs only when something the
// app runs needs a *Memory.
var Module = nano.Module("store", nano.Providers(NewMemory))

// Opened counts NewMemory calls, so tests can see an override skip it.
var Opened atomic.Int64

type Memory struct {
	mu    sync.Mutex
	names []string
}

// Started counts Start calls. Memory is a Starter, and a Starter that a
// consumer takes is built only when that consumer needs it: overriding the
// greeter.Store binding must skip both NewMemory and Start.
var Started atomic.Int64

// NewMemory returns a cleanup, which Build registers as a stop hook.
func NewMemory(log *slog.Logger) (*Memory, func(), error) {
	Opened.Add(1)
	m := &Memory{}
	return m, func() { log.Info("store closed") }, nil
}

// Start stands in for opening a real store, such as pinging a database.
func (m *Memory) Start(context.Context) error {
	Started.Add(1)
	return nil
}

func (m *Memory) Put(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.names = append(m.names, name)
	return nil
}

func (m *Memory) Names(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.names), nil
}
