//go:build integration

package redisx_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/redistest"
)

func TestMapReadsAndWritesFields(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	runners := redistest.New(t).Map[session]("runners")

	got, err := runners.Get(ctx, "r1")
	require.NoError(t, err)
	require.True(t, got.IsAbsent())

	require.NoError(t, runners.Set(ctx, "r1", session{User: "ana"}))
	require.NoError(t, runners.SetAll(ctx, map[string]session{"r2": {User: "bo"}, "r3": {User: "cy"}}))

	got, err = runners.Get(ctx, "r1")
	require.NoError(t, err)
	require.Equal(t, "ana", got.Value().User)

	all, err := runners.All(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]session{"r1": {User: "ana"}, "r2": {User: "bo"}, "r3": {User: "cy"}}, all)

	length, err := runners.Len(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, length)

	has, err := runners.Has(ctx, "r2")
	require.NoError(t, err)
	require.True(t, has)

	removed, err := runners.Delete(ctx, "r2", "missing")
	require.NoError(t, err)
	require.Equal(t, 1, removed)

	has, err = runners.Has(ctx, "r2")
	require.NoError(t, err)
	require.False(t, has)
}

func TestMapEmptyWritesAreNoOps(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	m := redistest.New(t).Map[int]("empty")

	require.NoError(t, m.SetAll(ctx, nil))
	removed, err := m.Delete(ctx)
	require.NoError(t, err)
	require.Zero(t, removed)

	all, err := m.All(ctx)
	require.NoError(t, err)
	require.Empty(t, all)
}

func TestMapSetIfAbsentKeepsTheFirstValue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	m := redistest.New(t).Map[string]("owners")

	written, err := m.SetIfAbsent(ctx, "doc", "ana")
	require.NoError(t, err)
	require.True(t, written)

	written, err = m.SetIfAbsent(ctx, "doc", "bo")
	require.NoError(t, err)
	require.False(t, written)

	got, err := m.Get(ctx, "doc")
	require.NoError(t, err)
	require.Equal(t, "ana", got.Value())
}

func TestMapWithTTLRefreshesTheExpiryOnEveryWrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	m := rx.Map[int]("presence").WithTTL(400 * time.Millisecond)

	require.NoError(t, m.Set(ctx, "a", 1))
	time.Sleep(250 * time.Millisecond)
	_, err := m.SetIfAbsent(ctx, "b", 2)
	require.NoError(t, err)
	time.Sleep(250 * time.Millisecond)

	// 500ms after the first write, the second write kept the map alive.
	length, err := m.Len(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, length)

	require.Eventually(t, func() bool {
		length, err := m.Len(ctx)
		return err == nil && length == 0
	}, 2*time.Second, 20*time.Millisecond)
}

func TestMapClearRemovesEveryField(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	m := redistest.New(t).Map[int]("cleared")

	require.NoError(t, m.SetAll(ctx, map[string]int{"a": 1, "b": 2}))
	require.NoError(t, m.Clear(ctx))
	length, err := m.Len(ctx)
	require.NoError(t, err)
	require.Zero(t, length)
}

func TestMapGetReportsWhatItCannotDecode(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	m := rx.Map[session]("corrupt")

	require.NoError(t, rx.Redis().HSet(ctx, m.Key(), "f", "not json").Err())
	_, err := m.Get(ctx, "f")
	require.ErrorContains(t, err, "decode redisx_test.session")
	_, err = m.All(ctx)
	require.ErrorContains(t, err, "decode redisx_test.session")
}

func TestSetAddsMembersOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	online := redistest.New(t).Set[string]("online")

	added, err := online.Add(ctx, "ana", "bo", "ana")
	require.NoError(t, err)
	require.Equal(t, 2, added)

	added, err = online.Add(ctx, "bo", "cy")
	require.NoError(t, err)
	require.Equal(t, 1, added)

	members, err := online.Members(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ana", "bo", "cy"}, members)

	found, err := online.Contains(ctx, "bo")
	require.NoError(t, err)
	require.True(t, found)

	removed, err := online.Remove(ctx, "bo", "nobody")
	require.NoError(t, err)
	require.Equal(t, 1, removed)

	length, err := online.Len(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, length)

	found, err = online.Contains(ctx, "bo")
	require.NoError(t, err)
	require.False(t, found)
}

func TestSetComparesStructMembersByValue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	set := redistest.New(t).Set[session]("sessions")

	added, err := set.Add(ctx, session{User: "ana", Roles: []string{"admin"}})
	require.NoError(t, err)
	require.Equal(t, 1, added)

	found, err := set.Contains(ctx, session{User: "ana", Roles: []string{"admin"}})
	require.NoError(t, err)
	require.True(t, found)

	added, err = set.Add(ctx, session{User: "ana", Roles: []string{"admin"}})
	require.NoError(t, err)
	require.Zero(t, added)
}

func TestSetEmptyCallsAreNoOps(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	set := redistest.New(t).Set[int]("empty")

	added, err := set.Add(ctx)
	require.NoError(t, err)
	require.Zero(t, added)
	removed, err := set.Remove(ctx)
	require.NoError(t, err)
	require.Zero(t, removed)
	members, err := set.Members(ctx)
	require.NoError(t, err)
	require.Empty(t, members)
}

func TestSetWithTTLAndClear(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	set := rx.Set[int]("ttl").WithTTL(time.Minute)

	_, err := set.Add(ctx, 1, 2)
	require.NoError(t, err)
	ttl, err := rx.Redis().PTTL(ctx, set.Key()).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 50*time.Second)

	require.NoError(t, set.Clear(ctx))
	length, err := set.Len(ctx)
	require.NoError(t, err)
	require.Zero(t, length)
}

func TestQueueIsFirstInFirstOut(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	jobs := redistest.New(t).Queue[session]("jobs")

	length, err := jobs.Push(ctx, session{User: "a"}, session{User: "b"})
	require.NoError(t, err)
	require.Equal(t, 2, length)
	length, err = jobs.Push(ctx, session{User: "c"})
	require.NoError(t, err)
	require.Equal(t, 3, length)

	front, err := jobs.Peek(ctx)
	require.NoError(t, err)
	require.Equal(t, "a", front.Value().User)

	for _, want := range []string{"a", "b", "c"} {
		item, err := jobs.Pop(ctx)
		require.NoError(t, err)
		require.Equal(t, want, item.Value().User)
	}
	item, err := jobs.Pop(ctx)
	require.NoError(t, err)
	require.True(t, item.IsAbsent())

	front, err = jobs.Peek(ctx)
	require.NoError(t, err)
	require.True(t, front.IsAbsent())
}

func TestQueuePushWithNoItemsReturnsTheLength(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := redistest.New(t).Queue[int]("q")

	_, err := q.Push(ctx, 1, 2)
	require.NoError(t, err)
	length, err := q.Push(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, length)
}

func TestQueuePopWaitGetsAnItemPushedLater(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := redistest.New(t).Queue[string]("later")

	go func() {
		time.Sleep(200 * time.Millisecond)
		_, err := q.Push(ctx, "late")
		assert.NoError(t, err)
	}()
	start := time.Now()
	item, err := q.PopWait(ctx, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, "late", item.Value())
	require.Less(t, time.Since(start), 3*time.Second)
}

func TestQueuePopWaitIsAbsentAfterTheTimeout(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := redistest.New(t).Queue[string]("never")

	start := time.Now()
	item, err := q.PopWait(ctx, 10*time.Millisecond)
	require.NoError(t, err)
	require.True(t, item.IsAbsent())
	require.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond, "the wait rounds up to one second")
}

func TestQueueGivesEachItemToOneConsumer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	q := redistest.New(t).Queue[int]("work")
	items := make([]int, 200)
	for i := range items {
		items[i] = i
	}
	_, err := q.Push(ctx, items...)
	require.NoError(t, err)

	var mu sync.Mutex
	var seen []int
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for {
				item, err := q.Pop(ctx)
				if !assert.NoError(t, err) || item.IsAbsent() {
					return
				}
				mu.Lock()
				seen = append(seen, item.Value())
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	require.ElementsMatch(t, items, seen)
}

func TestQueueWithTTLAndClear(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	q := rx.Queue[int]("ttl").WithTTL(time.Minute)

	_, err := q.Push(ctx, 1)
	require.NoError(t, err)
	ttl, err := rx.Redis().PTTL(ctx, q.Key()).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 50*time.Second)

	require.NoError(t, q.Clear(ctx))
	length, err := q.Len(ctx)
	require.NoError(t, err)
	require.Zero(t, length)
}

func TestCounterCountsAtomically(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hits := redistest.New(t).Counter("hits")

	count, err := hits.Get(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, err := hits.Incr(ctx)
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	count, err = hits.Add(ctx, -10)
	require.NoError(t, err)
	require.Equal(t, int64(40), count)

	count, err = hits.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(40), count)

	require.NoError(t, hits.Reset(ctx))
	count, err = hits.Get(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestCounterWithTTLExpires(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	window := redistest.New(t).Counter("window").WithTTL(150 * time.Millisecond)

	_, err := window.Incr(ctx)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		count, err := window.Get(ctx)
		return err == nil && count == 0
	}, 2*time.Second, 20*time.Millisecond)
}

func TestCounterGetReportsANonIntegerValue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	c := rx.Counter("text")

	require.NoError(t, rx.Redis().Set(ctx, c.Key(), "abc", 0).Err())
	_, err := c.Get(ctx)
	require.Error(t, err)
	_, err = c.Incr(ctx)
	require.Error(t, err)
}
