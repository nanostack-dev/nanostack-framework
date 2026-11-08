package valuerecv

import "context"

type Worker struct{ n int }

// NewWorker returns a value, but Start has a pointer receiver.
func NewWorker() Worker                       { return Worker{} }
func (w *Worker) Start(context.Context) error { w.n++; return nil }

func Use(Worker) {}
