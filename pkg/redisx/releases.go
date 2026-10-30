package redisx

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// listenTimeout caps the wait for the server to confirm a subscription. Without
// a confirmation the waiting caller falls back to polling.
const listenTimeout = 2 * time.Second

// releases tells the Lock calls of one Client that a lease was released. Each
// mutex name with waiting callers has one pub/sub connection, shared by all of
// them and closed when the last one stops waiting.
type releases struct {
	rdb      redis.UniversalClient
	mu       sync.Mutex
	channels map[string]*releaseChannel
}

type releaseChannel struct {
	ready   chan struct{}
	pubsub  *redis.PubSub
	waiters map[chan struct{}]struct{}
}

func newReleases(rdb redis.UniversalClient) *releases {
	return &releases{rdb: rdb, channels: map[string]*releaseChannel{}}
}

// listen returns a channel that receives a value after each release on
// channel, once the server has confirmed the subscription. The channel is nil
// when the subscription failed. Call stop when you no longer wait.
func (r *releases) listen(ctx context.Context, channel string) (released <-chan struct{}, stop func()) {
	wake := make(chan struct{}, 1)
	r.mu.Lock()
	ch, found := r.channels[channel]
	if !found {
		ch = &releaseChannel{ready: make(chan struct{}), waiters: map[chan struct{}]struct{}{}}
		r.channels[channel] = ch
	}
	ch.waiters[wake] = struct{}{}
	r.mu.Unlock()
	stop = func() { r.leave(channel, ch, wake) }

	if !found {
		r.subscribe(ctx, channel, ch)
	}
	select {
	case <-ch.ready:
	case <-ctx.Done():
		return nil, stop
	}
	r.mu.Lock()
	subscribed := ch.pubsub != nil
	r.mu.Unlock()
	if !subscribed {
		return nil, stop
	}
	return wake, stop
}

func (r *releases) subscribe(ctx context.Context, channel string, ch *releaseChannel) {
	defer close(ch.ready)
	confirmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), listenTimeout)
	defer cancel()
	pubsub := r.rdb.Subscribe(confirmCtx, channel)
	if _, err := pubsub.ReceiveTimeout(confirmCtx, listenTimeout); err != nil {
		_ = pubsub.Close()
		r.mu.Lock()
		if r.channels[channel] == ch {
			delete(r.channels, channel)
		}
		r.mu.Unlock()
		return
	}
	messages := pubsub.Channel()
	r.mu.Lock()
	ch.pubsub = pubsub
	r.mu.Unlock()
	go func() {
		for range messages {
			r.mu.Lock()
			for wake := range ch.waiters {
				select {
				case wake <- struct{}{}:
				default:
				}
			}
			r.mu.Unlock()
		}
	}()
}

func (r *releases) leave(channel string, ch *releaseChannel, wake chan struct{}) {
	r.mu.Lock()
	delete(ch.waiters, wake)
	var closing *redis.PubSub
	if len(ch.waiters) == 0 && ch.pubsub != nil {
		closing = ch.pubsub
		ch.pubsub = nil
		if r.channels[channel] == ch {
			delete(r.channels, channel)
		}
	}
	r.mu.Unlock()
	if closing != nil {
		_ = closing.Close()
	}
}
