// Package workflow mirrors pgkit's worker: it takes every workflow
// definition the features contribute (group "pgkit.workflow.definitions").
package workflow

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// Module provides the worker and declares an empty collection of crons, so
// the worker builds whether or not the app adds any.
var Module = nano.Module("workflow", nano.Providers(NewWorker), nano.Collect[*Cron]())

type Definition struct{ Name string }

type Cron struct{ Spec string }

// Worker is a Runner, so it is a root of the graph.
type Worker struct {
	Defs  []*Definition
	Crons []*Cron
}

func NewWorker(defs []*Definition, crons []*Cron) *Worker { return &Worker{Defs: defs, Crons: crons} }

func (w *Worker) Run(ctx context.Context) error { <-ctx.Done(); return nil }
