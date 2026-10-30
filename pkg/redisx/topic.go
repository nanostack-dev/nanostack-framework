package redisx

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Topic is a typed Redis pub/sub channel.
//
// Delivery is at most once: a subscriber gets only the messages published
// while it is connected, and a message is gone once handled or lost.
type Topic[T any] struct {
	rdb     redis.UniversalClient
	channel string
	codec   Codec[T]
	onError func(ctx context.Context, err error)
}

// Topic returns the channel called name.
func (c *Client) Topic[T any](name string) Topic[T] {
	return Topic[T]{rdb: c.rdb, channel: c.Key(name), codec: defaultCodec[T]()}
}

// WithCodec replaces the default codec.
func (t Topic[T]) WithCodec(codec Codec[T]) Topic[T] {
	t.codec = codec
	return t
}

// OnError receives the messages a subscriber could not decode and the errors
// its handler returned. The subscription continues after each one. Without
// OnError they are logged at error level with the context logger.
func (t Topic[T]) OnError(fn func(ctx context.Context, err error)) Topic[T] {
	t.onError = fn
	return t
}

// Channel returns the full channel name.
func (t Topic[T]) Channel() string { return t.channel }

// Publish sends msg and returns how many subscribers received it.
func (t Topic[T]) Publish(ctx context.Context, msg T) (int, error) {
	raw, err := t.codec.Encode(msg)
	if err != nil {
		return 0, err
	}
	receivers, err := t.rdb.Publish(ctx, t.channel, raw).Result()
	if err != nil {
		return 0, fmt.Errorf("redisx: publish to %s: %w", t.channel, err)
	}
	return int(receivers), nil
}

// Subscribe calls handler for each message, one at a time and in order, until
// ctx ends or the Subscription closes. Subscribe returns once the server has
// confirmed the subscription, so every message published after it returns is
// delivered. After a lost connection, the client subscribes again on its own;
// messages published in between are lost.
func (t Topic[T]) Subscribe(ctx context.Context, handler func(ctx context.Context, msg T) error) (*Subscription, error) {
	pubsub := t.rdb.Subscribe(ctx, t.channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("redisx: subscribe to %s: %w", t.channel, err)
	}
	subCtx, cancel := context.WithCancel(ctx)
	sub := &Subscription{cancel: cancel, done: make(chan struct{})}
	messages := pubsub.Channel()
	go func() {
		defer close(sub.done)
		defer func() { sub.err = pubsub.Close() }()
		for {
			select {
			case <-subCtx.Done():
				return
			case message, ok := <-messages:
				if !ok {
					return
				}
				t.deliver(subCtx, message.Payload, handler)
			}
		}
	}()
	return sub, nil
}

func (t Topic[T]) deliver(ctx context.Context, raw string, handler func(ctx context.Context, msg T) error) {
	msg, err := t.codec.Decode(raw)
	if err == nil {
		err = handler(ctx, msg)
	}
	if err == nil || (ctx.Err() != nil && errors.Is(err, ctx.Err())) {
		return
	}
	err = fmt.Errorf("redisx: handle message on %s: %w", t.channel, err)
	if t.onError != nil {
		t.onError(ctx, err)
		return
	}
	zerolog.Ctx(ctx).Error().Err(err).Str("channel", t.channel).Msg("pub/sub message not handled")
}

// Subscription is a running subscription to a Topic.
type Subscription struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Done closes when the subscription has stopped: after Close, or when the
// context given to Subscribe ends.
func (s *Subscription) Done() <-chan struct{} { return s.done }

// Close stops the subscription and waits until the handler has returned. It is
// safe to call more than once.
func (s *Subscription) Close() error {
	s.cancel()
	<-s.done
	return s.err
}
