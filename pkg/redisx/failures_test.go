package redisx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
)

var errBadValue = errors.New("bad value")

// failingCodec refuses every value, to prove writes stop before the server.
type failingCodec struct{}

func (failingCodec) Encode(string) (string, error) { return "", errBadValue }
func (failingCodec) Decode(string) (string, error) { return "", errBadValue }

func closedClient(t *testing.T) *redisx.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{})
	require.NoError(t, rdb.Close())
	return redisx.New(rdb, redisx.WithPrefix("app"))
}

func TestEveryCallReportsTheServerErrorWithItsKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rx := closedClient(t)
	value := rx.Value[string]("v")
	m := rx.Map[string]("m")
	set := rx.Set[string]("s")
	queue := rx.Queue[string]("q")
	counter := rx.Counter("c")
	topic := rx.Topic[string]("t")
	mutex := rx.Mutex("x")

	calls := map[string]func() error{
		"value get":           func() error { _, err := value.Get(ctx); return err },
		"value set":           func() error { return value.Set(ctx, "a") },
		"value set if absent": func() error { _, err := value.SetIfAbsent(ctx, "a"); return err },
		"value swap":          func() error { _, err := value.Swap(ctx, "a"); return err },
		"value delete":        func() error { _, err := value.Delete(ctx); return err },
		"value exists":        func() error { _, err := value.Exists(ctx); return err },
		"value update": func() error {
			_, err := value.Update(ctx, func(functional.Option[string]) (string, error) { return "a", nil })
			return err
		},
		"map get":           func() error { _, err := m.Get(ctx, "f"); return err },
		"map set":           func() error { return m.Set(ctx, "f", "a") },
		"map set if absent": func() error { _, err := m.SetIfAbsent(ctx, "f", "a"); return err },
		"map delete":        func() error { _, err := m.Delete(ctx, "f"); return err },
		"map has":           func() error { _, err := m.Has(ctx, "f"); return err },
		"map len":           func() error { _, err := m.Len(ctx); return err },
		"map all":           func() error { _, err := m.All(ctx); return err },
		"map clear":         func() error { return m.Clear(ctx) },
		"set add":           func() error { _, err := set.Add(ctx, "a"); return err },
		"set remove":        func() error { _, err := set.Remove(ctx, "a"); return err },
		"set contains":      func() error { _, err := set.Contains(ctx, "a"); return err },
		"set members":       func() error { _, err := set.Members(ctx); return err },
		"set len":           func() error { _, err := set.Len(ctx); return err },
		"set clear":         func() error { return set.Clear(ctx) },
		"queue push":        func() error { _, err := queue.Push(ctx, "a"); return err },
		"queue push none":   func() error { _, err := queue.Push(ctx); return err },
		"queue pop":         func() error { _, err := queue.Pop(ctx); return err },
		"queue peek":        func() error { _, err := queue.Peek(ctx); return err },
		"queue len":         func() error { _, err := queue.Len(ctx); return err },
		"queue clear":       func() error { return queue.Clear(ctx) },
		"counter add":       func() error { _, err := counter.Incr(ctx); return err },
		"counter get":       func() error { _, err := counter.Get(ctx); return err },
		"counter reset":     func() error { return counter.Reset(ctx) },
		"topic publish":     func() error { _, err := topic.Publish(ctx, "a"); return err },
		"topic subscribe": func() error {
			_, err := topic.Subscribe(ctx, func(context.Context, string) error { return nil })
			return err
		},
		"mutex try lock": func() error { _, err := mutex.TryLock(ctx); return err },
		"mutex lock":     func() error { _, err := mutex.Lock(ctx); return err },
		"mutex do":       func() error { return mutex.Do(ctx, func(context.Context) error { return nil }) },
		"mutex try do":   func() error { return mutex.TryDo(ctx, func(context.Context) error { return nil }) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			require.ErrorIs(t, err, redis.ErrClosed)
			require.ErrorContains(t, err, "redisx: ")
			require.NotErrorIs(t, err, redisx.ErrLocked)
		})
	}
}

func TestWritesStopAtACodecError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rx := closedClient(t)
	codec := failingCodec{}
	value := rx.Value[string]("v").WithCodec(codec)
	m := rx.Map[string]("m").WithCodec(codec)
	set := rx.Set[string]("s").WithCodec(codec)
	queue := rx.Queue[string]("q").WithCodec(codec)
	topic := rx.Topic[string]("t").WithCodec(codec)

	calls := map[string]func() error{
		"value set":           func() error { return value.Set(ctx, "a") },
		"value set if absent": func() error { _, err := value.SetIfAbsent(ctx, "a"); return err },
		"value swap":          func() error { _, err := value.Swap(ctx, "a"); return err },
		"map set":             func() error { return m.Set(ctx, "f", "a") },
		"map set if absent":   func() error { _, err := m.SetIfAbsent(ctx, "f", "a"); return err },
		"set add":             func() error { _, err := set.Add(ctx, "a"); return err },
		"set remove":          func() error { _, err := set.Remove(ctx, "a"); return err },
		"set contains":        func() error { _, err := set.Contains(ctx, "a"); return err },
		"queue push":          func() error { _, err := queue.Push(ctx, "a"); return err },
		"topic publish":       func() error { _, err := topic.Publish(ctx, "a"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, call(), errBadValue)
		})
	}
}
