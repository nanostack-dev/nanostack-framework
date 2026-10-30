//go:build integration

package redisx_test

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/redistest"
)

// errorsOf collects what a Topic gives to OnError.
func errorsOf() (func(context.Context, error), <-chan error) {
	errs := make(chan error, 1000)
	return func(_ context.Context, err error) {
		select {
		case errs <- err:
		default:
		}
	}, errs
}

func waitForError(t *testing.T, errs <-chan error, target error) error {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case err := <-errs:
			if errors.Is(err, target) {
				return err
			}
		case <-timeout:
			t.Fatalf("no error matching %v", target)
		}
	}
}

// publishUntilDelivered publishes msg until a subscriber receives it: a
// subscription that came back may need a moment before the server counts it.
func publishUntilDelivered(t *testing.T, topic redisx.Topic[string], msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		receivers, err := topic.Publish(t.Context(), msg)
		return err == nil && receivers == 1
	}, 10*time.Second, 50*time.Millisecond)
}

func TestTopicReportsTheMessagesLostToACutConnection(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	onError, errs := errorsOf()
	topic := server.New(t).Topic[string]("cut").OnError(onError)
	var got collect[string]
	sub, err := topic.Subscribe(t.Context(), got.handle)
	require.NoError(t, err)
	defer sub.Close()

	server.CutConnections(t)
	waitForError(t, errs, redisx.ErrResubscribed)

	publishUntilDelivered(t, topic, "after")
	require.Eventually(t, func() bool { return len(got.snapshot()) > 0 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "after", got.snapshot()[0])
}

func TestTopicSubscribesAgainAfterARestart(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	onError, errs := errorsOf()
	topic := server.New(t).Topic[string]("restart").OnError(onError)
	var got collect[string]
	sub, err := topic.Subscribe(t.Context(), got.handle)
	require.NoError(t, err)
	defer sub.Close()

	server.Restart(t)
	waitForError(t, errs, redisx.ErrResubscribed)

	publishUntilDelivered(t, topic, "after")
	require.Eventually(t, func() bool { return len(got.snapshot()) > 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestTopicReportsASlowSubscriberTheServerDropped(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	ctx := t.Context()
	rx := server.New(t)
	require.NoError(t, rx.Redis().ConfigSet(ctx, "client-output-buffer-limit", "pubsub 256kb 128kb 1").Err())
	onError, errs := errorsOf()
	topic := rx.Topic[string]("slow").OnError(onError)

	unblock := make(chan struct{})
	sub, err := topic.Subscribe(ctx, func(ctx context.Context, msg string) error {
		<-unblock
		return nil
	})
	require.NoError(t, err)
	defer sub.Close()

	// Publish until the server gives up on the stuck subscriber.
	payload := strings.Repeat("x", 4096)
	dropped := false
	for range 200 {
		pipe := rx.Redis().Pipeline()
		for range 500 {
			pipe.Publish(ctx, topic.Channel(), payload)
		}
		last := pipe.Publish(ctx, topic.Channel(), payload)
		_, err := pipe.Exec(ctx)
		require.NoError(t, err)
		if last.Val() == 0 {
			dropped = true
			break
		}
	}
	require.True(t, dropped, "the server never dropped the subscriber")
	close(unblock)

	waitForError(t, errs, redisx.ErrResubscribed)
}

func TestTopicContinuesAfterAHandlerPanics(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	onError, errs := errorsOf()
	topic := redistest.New(t).Topic[string]("panics").OnError(onError)
	var got collect[string]
	sub, err := topic.Subscribe(ctx, func(ctx context.Context, msg string) error {
		if msg == "boom" {
			panic("boom")
		}
		return got.handle(ctx, msg)
	})
	require.NoError(t, err)
	defer sub.Close()

	for _, msg := range []string{"a", "boom", "b"} {
		_, err := topic.Publish(ctx, msg)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return len(got.snapshot()) == 2 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"a", "b"}, got.snapshot())
	require.ErrorContains(t, <-errs, "handler panicked: boom")
}

func TestTopicWithAShortChannelNameDeliversAtOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	// Without a prefix, the subscribe confirmation and each message are
	// shorter than 36 bytes. go-redis 9.21 waited for more bytes.
	topic := redisx.New(redistest.Client(t)).Topic[string](rand.Text()[:2])
	received := make(chan time.Time, 1)

	start := time.Now()
	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sub, err := topic.Subscribe(subCtx, func(context.Context, string) error {
		received <- time.Now()
		return nil
	})
	require.NoError(t, err)
	defer sub.Close()
	require.Less(t, time.Since(start), time.Second)

	published := time.Now()
	_, err = topic.Publish(ctx, "x")
	require.NoError(t, err)
	select {
	case at := <-received:
		require.Less(t, at.Sub(published), time.Second)
	case <-time.After(5 * time.Second):
		t.Fatal("the message never came")
	}
}
