//go:build integration

package redisx_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/redistest"
)

func TestValueIsAbsentUntilSet(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[session]("session")

	got, err := value.Get(ctx)
	require.NoError(t, err)
	require.True(t, got.IsAbsent())

	require.NoError(t, value.Set(ctx, session{User: "ana"}))
	got, err = value.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, session{User: "ana"}, got.Value())
}

func TestValueStoresStringsAsText(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[string]("greeting")

	require.NoError(t, value.Set(ctx, "hello"))
	raw, err := rx.Redis().Get(ctx, value.Key()).Result()
	require.NoError(t, err)
	require.Equal(t, "hello", raw)
}

func TestValueWithCodecUsesIt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[string]("quoted").WithCodec(redisx.JSON[string]())

	require.NoError(t, value.Set(ctx, "hello"))
	raw, err := rx.Redis().Get(ctx, value.Key()).Result()
	require.NoError(t, err)
	require.Equal(t, `"hello"`, raw)
}

func TestValueWithTTLExpires(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[int]("short").WithTTL(150 * time.Millisecond)

	require.NoError(t, value.Set(ctx, 1))
	ttl, err := rx.Redis().PTTL(ctx, value.Key()).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, 150*time.Millisecond)

	require.Eventually(t, func() bool {
		exists, err := value.Exists(ctx)
		return err == nil && !exists
	}, 2*time.Second, 20*time.Millisecond)
}

func TestValueSetWithoutTTLRemovesAnEarlierExpiry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[int]("persist")

	require.NoError(t, value.WithTTL(time.Minute).Set(ctx, 1))
	require.NoError(t, value.Set(ctx, 2))
	ttl, err := rx.Redis().PTTL(ctx, value.Key()).Result()
	require.NoError(t, err)
	require.Equal(t, time.Duration(-1), ttl)
}

func TestValueSetIfAbsentWritesOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[string]("owner")

	written, err := value.SetIfAbsent(ctx, "first")
	require.NoError(t, err)
	require.True(t, written)

	written, err = value.SetIfAbsent(ctx, "second")
	require.NoError(t, err)
	require.False(t, written)

	got, err := value.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "first", got.Value())
}

func TestValueSetIfAbsentHasOneWinnerAmongRacers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[int]("race")

	var wins sync.Map
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			written, err := value.SetIfAbsent(ctx, i)
			if err == nil && written {
				wins.Store(i, true)
			}
		})
	}
	wg.Wait()
	count := 0
	wins.Range(func(any, any) bool { count++; return true })
	require.Equal(t, 1, count)
}

func TestValueSwapReturnsWhatItReplaced(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[string]("state")

	previous, err := value.Swap(ctx, "one")
	require.NoError(t, err)
	require.True(t, previous.IsAbsent())

	previous, err = value.Swap(ctx, "two")
	require.NoError(t, err)
	require.Equal(t, "one", previous.Value())
}

func TestValueDeleteReportsWhetherItExisted(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[string]("gone")

	removed, err := value.Delete(ctx)
	require.NoError(t, err)
	require.False(t, removed)

	require.NoError(t, value.Set(ctx, "x"))
	removed, err = value.Delete(ctx)
	require.NoError(t, err)
	require.True(t, removed)

	exists, err := value.Exists(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestValueGetReportsWhatItCannotDecode(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[session]("corrupt")

	require.NoError(t, rx.Redis().Set(ctx, value.Key(), "not json", 0).Err())
	_, err := value.Get(ctx)
	require.ErrorContains(t, err, "decode redisx_test.session")
}

func TestValueUpdateStartsFromAbsence(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[int]("fresh")

	next, err := value.Update(ctx, func(current functional.Option[int]) (int, error) {
		require.True(t, current.IsAbsent())
		return 10, nil
	})
	require.NoError(t, err)
	require.Equal(t, 10, next)
}

func TestValueUpdateLosesNoWriteUnderContention(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[[]int]("appends")

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			_, err := value.Update(ctx, func(current functional.Option[[]int]) ([]int, error) {
				return append(current.OrElse(nil), i), nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	got, err := value.Get(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, got.Value())
}

func TestValueUpdateKeepsTheExpiry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[int]("kept")

	require.NoError(t, value.WithTTL(time.Minute).Set(ctx, 1))
	_, err := value.Update(ctx, func(current functional.Option[int]) (int, error) {
		return current.Value() + 1, nil
	})
	require.NoError(t, err)
	ttl, err := rx.Redis().PTTL(ctx, value.Key()).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 50*time.Second)
}

func TestValueUpdateWithTTLSetsIt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[int]("renewed").WithTTL(time.Minute)

	_, err := value.Update(ctx, func(functional.Option[int]) (int, error) { return 1, nil })
	require.NoError(t, err)
	ttl, err := rx.Redis().PTTL(ctx, value.Key()).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 50*time.Second)
}

func TestValueUpdateReturnsTheCallbackErrorAndWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	value := redistest.New(t).Value[int]("refused")
	refused := errors.New("refused")

	_, err := value.Update(ctx, func(functional.Option[int]) (int, error) { return 0, refused })
	require.ErrorIs(t, err, refused)
	exists, err := value.Exists(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestValueUpdateGivesUpWhenEveryAttemptConflicts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	value := rx.Value[string]("contended")
	attempts := 0

	_, err := value.Update(ctx, func(functional.Option[string]) (string, error) {
		attempts++
		// Another writer changes the value between the read and the write.
		require.NoError(t, rx.Redis().Set(ctx, value.Key(), strings.Repeat("x", attempts), 0).Err())
		return "mine", nil
	})
	require.ErrorIs(t, err, redisx.ErrConflict)
	require.Equal(t, 32, attempts)
}
