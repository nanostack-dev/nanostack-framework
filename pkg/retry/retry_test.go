package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pkg/retry"
)

func TestDoSucceedsFirstAttempt(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), retry.Default(), func(context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDoSucceedsOnLaterAttempt(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), retry.Options{MaxAttempts: 3, Delay: time.Millisecond}, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDoExhaustsAttemptsAndReturnsLastError(t *testing.T) {
	calls := 0
	want := errors.New("boom-3")
	err := retry.Do(context.Background(), retry.Options{MaxAttempts: 3, Delay: time.Millisecond}, func(context.Context) error {
		calls++
		return errors.New("boom-" + string(rune('0'+calls)))
	})
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("expected last error %v, got %v", want, err)
	}
}

func TestDoStopsOnNonRetryableError(t *testing.T) {
	calls := 0
	sentinel := errors.New("fatal")
	err := retry.Do(context.Background(), retry.Options{
		MaxAttempts: 5,
		Delay:       time.Millisecond,
		RetryIf:     func(e error) bool { return !errors.Is(e, sentinel) },
	}, func(context.Context) error {
		calls++
		return sentinel
	})
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", calls)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestDoStopsWhenContextCancelledDuringDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := retry.Do(ctx, retry.Options{MaxAttempts: 5, Delay: time.Hour}, func(context.Context) error {
		calls++
		return errors.New("transient")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call before cancel, got %d", calls)
	}
}

func TestDoNormalizesZeroOptions(t *testing.T) {
	calls := 0
	// MaxAttempts 0 -> normalized to 1: fn runs once, no retry.
	err := retry.Do(context.Background(), retry.Options{}, func(context.Context) error {
		calls++
		return errors.New("x")
	})
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDoReturnsImmediatelyOnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := retry.Do(ctx, retry.Default(), func(context.Context) error {
		calls++
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected 0 calls on cancelled context, got %d", calls)
	}
}
