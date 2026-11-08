// Package lib is a library module: its unused providers are skipped.
package lib

import (
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

var Module = nano.Module("lib", nano.Providers(NewClock, NewUnused))

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// NewClock returns an interface, so Build registers its lifecycle with
// App.Manage at run time.
func NewClock() Clock { return systemClock{} }

type Unused struct{}

func NewUnused() *Unused { return &Unused{} }
