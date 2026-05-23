package queueworker

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestStartStop(t *testing.T) {
	started := Start(context.Background(), blockingRunner{}, zerolog.Nop(), "test")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Stop(ctx, started); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}
