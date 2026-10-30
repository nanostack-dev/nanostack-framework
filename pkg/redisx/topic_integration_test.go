//go:build integration

package redisx_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/redistest"
)

type event struct {
	Seq  int    `json:"seq"`
	Kind string `json:"kind"`
}

// collect gathers messages from a subscription for assertions.
type collect[T any] struct {
	mu       sync.Mutex
	messages []T
}

func (c *collect[T]) handle(_ context.Context, msg T) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, msg)
	return nil
}

func (c *collect[T]) snapshot() []T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]T(nil), c.messages...)
}

func TestTopicDeliversTypedMessagesInOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	topic := redistest.New(t).Topic[event]("events")
	var got collect[event]

	sub, err := topic.Subscribe(ctx, got.handle)
	require.NoError(t, err)
	defer func() { require.NoError(t, sub.Close()) }()

	var want []event
	for i := range 50 {
		msg := event{Seq: i, Kind: "created"}
		want = append(want, msg)
		receivers, err := topic.Publish(ctx, msg)
		require.NoError(t, err)
		require.Equal(t, 1, receivers)
	}
	require.Eventually(t, func() bool { return len(got.snapshot()) == 50 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, want, got.snapshot())
}

func TestTopicFansOutToEverySubscriber(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	topic := redistest.New(t).Topic[string]("broadcast")
	var first, second collect[string]

	subA, err := topic.Subscribe(ctx, first.handle)
	require.NoError(t, err)
	defer subA.Close()
	subB, err := topic.Subscribe(ctx, second.handle)
	require.NoError(t, err)
	defer subB.Close()

	receivers, err := topic.Publish(ctx, "hello")
	require.NoError(t, err)
	require.Equal(t, 2, receivers)
	require.Eventually(t, func() bool {
		return len(first.snapshot()) == 1 && len(second.snapshot()) == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"hello"}, first.snapshot())
	require.Equal(t, []string{"hello"}, second.snapshot())
}

func TestTopicDropsMessagesWithoutSubscribers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	topic := redistest.New(t).Topic[string]("nobody")

	receivers, err := topic.Publish(ctx, "lost")
	require.NoError(t, err)
	require.Zero(t, receivers)

	var got collect[string]
	sub, err := topic.Subscribe(ctx, got.handle)
	require.NoError(t, err)
	defer sub.Close()
	_, err = topic.Publish(ctx, "kept")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(got.snapshot()) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"kept"}, got.snapshot())
}

func TestTopicPrefixesSeparateChannels(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mine := redistest.New(t).Topic[string]("same")
	theirs := redistest.New(t).Topic[string]("same")
	var got collect[string]

	sub, err := mine.Subscribe(ctx, got.handle)
	require.NoError(t, err)
	defer sub.Close()

	receivers, err := theirs.Publish(ctx, "other app")
	require.NoError(t, err)
	require.Zero(t, receivers)
}

func TestTopicReportsBadMessagesAndKeepsGoing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	topic := rx.Topic[event]("mixed")
	var got collect[event]
	var failures collect[error]
	refused := errors.New("refused")

	sub, err := topic.
		OnError(func(ctx context.Context, err error) { _ = failures.handle(ctx, err) }).
		Subscribe(ctx, func(ctx context.Context, msg event) error {
			if msg.Kind == "refuse" {
				return refused
			}
			return got.handle(ctx, msg)
		})
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, rx.Redis().Publish(ctx, topic.Channel(), "not json").Err())
	_, err = topic.Publish(ctx, event{Seq: 1, Kind: "refuse"})
	require.NoError(t, err)
	_, err = topic.Publish(ctx, event{Seq: 2, Kind: "ok"})
	require.NoError(t, err)

	require.Eventually(t, func() bool { return len(got.snapshot()) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []event{{Seq: 2, Kind: "ok"}}, got.snapshot())
	errs := failures.snapshot()
	require.Len(t, errs, 2)
	require.ErrorContains(t, errs[0], "decode redisx_test.event")
	require.ErrorIs(t, errs[1], refused)
	require.ErrorContains(t, errs[1], topic.Channel())
}

func TestTopicWithCodecUsesIt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	topic := rx.Topic[string]("json").WithCodec(redisx.JSON[string]())
	raw := rx.Topic[string]("json")
	var encoded collect[string]

	sub, err := raw.Subscribe(ctx, encoded.handle)
	require.NoError(t, err)
	defer sub.Close()

	_, err = topic.Publish(ctx, "hi")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(encoded.snapshot()) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{`"hi"`}, encoded.snapshot())
}

func TestSubscriptionCloseWaitsForTheHandlerAndStopsDelivery(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	topic := redistest.New(t).Topic[int]("closing")
	started := make(chan struct{})
	var finished, calls atomic.Int32

	sub, err := topic.Subscribe(ctx, func(context.Context, int) error {
		if calls.Add(1) == 1 {
			close(started)
		}
		time.Sleep(200 * time.Millisecond)
		finished.Add(1)
		return nil
	})
	require.NoError(t, err)

	_, err = topic.Publish(ctx, 1)
	require.NoError(t, err)
	<-started
	require.NoError(t, sub.Close())
	require.Equal(t, int32(1), finished.Load(), "Close returned before the handler")
	require.NoError(t, sub.Close(), "a second Close is safe")

	_, err = topic.Publish(ctx, 2)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int32(1), calls.Load(), "the handler ran after Close")
	select {
	case <-sub.Done():
	default:
		t.Fatal("Done is open after Close")
	}
}

func TestSubscriptionEndsWithItsContext(t *testing.T) {
	t.Parallel()
	topic := redistest.New(t).Topic[int]("scoped")
	ctx, cancel := context.WithCancel(t.Context())

	sub, err := topic.Subscribe(ctx, func(context.Context, int) error { return nil })
	require.NoError(t, err)
	cancel()

	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription outlived its context")
	}
	require.NoError(t, sub.Close())
	require.Eventually(t, func() bool {
		receivers, err := topic.Publish(t.Context(), 1)
		return err == nil && receivers == 0
	}, 5*time.Second, 20*time.Millisecond, "the server still has the subscriber")
}

func TestSubscribeFailsOnAnEndedContext(t *testing.T) {
	t.Parallel()
	topic := redistest.New(t).Topic[int]("never")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := topic.Subscribe(ctx, func(context.Context, int) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
}
