//go:build integration

package redistest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/redistest"
)

func TestAddrStartsOneServerForTheWholeBinary(t *testing.T) {
	require.Equal(t, redistest.Addr(t), redistest.Addr(t))
	require.NoError(t, redistest.Client(t).Ping(t.Context()).Err())
}

func TestNamespacesAreUnique(t *testing.T) {
	first, second := redistest.Namespace(t), redistest.Namespace(t)
	require.NotEqual(t, first, second)
	require.True(t, strings.HasPrefix(first, "test-"))
}

func TestNamespaceDeletesItsKeysWhenTheTestEnds(t *testing.T) {
	var namespace string
	var mutexKey string
	t.Run("writer", func(t *testing.T) {
		rx := redistest.New(t)
		namespace = strings.TrimSuffix(rx.Key(""), ":")
		mutexKey = "{" + rx.Key("lock") + "}:fence"
		require.NoError(t, rx.Value[string]("a").Set(t.Context(), "x"))
		_, err := rx.Mutex("lock").TryLock(t.Context())
		require.NoError(t, err)
	})

	rdb := redistest.Client(t)
	keys, err := rdb.Keys(t.Context(), "*"+namespace+"*").Result()
	require.NoError(t, err)
	require.Empty(t, keys)
	require.ErrorIs(t, rdb.Get(t.Context(), mutexKey).Err(), redis.Nil)
}

func TestNamespaceLeavesOtherKeysAlone(t *testing.T) {
	rdb := redistest.Client(t)
	outside := "outside-" + redistest.Namespace(t)[5:]
	require.NoError(t, rdb.Set(t.Context(), outside+"-kept", "1", 0).Err())
	t.Cleanup(func() { _ = rdb.Del(t.Context(), outside+"-kept").Err() })

	t.Run("other", func(t *testing.T) {
		require.NoError(t, redistest.New(t).Value[int]("x").Set(t.Context(), 1))
	})
	require.NoError(t, rdb.Get(t.Context(), outside+"-kept").Err())
}

func TestServerRestartKeepsTheAddressAndLosesTheKeys(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	rdb := server.Client(t)
	require.NoError(t, rdb.Set(t.Context(), "k", "v", 0).Err())
	addr := server.Addr()

	server.Restart(t)
	require.Equal(t, addr, server.Addr())
	require.ErrorIs(t, rdb.Get(t.Context(), "k").Err(), redis.Nil)
}

func TestServerPauseStopsTheAnswersUntilResume(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr(), ReadTimeout: 300 * time.Millisecond, MaxRetries: -1})
	defer rdb.Close()
	require.NoError(t, rdb.Ping(t.Context()).Err())

	server.Pause(t)
	require.Error(t, rdb.Ping(t.Context()).Err())
	server.Resume(t)
	require.Eventually(t, func() bool { return rdb.Ping(t.Context()).Err() == nil }, 5*time.Second, 50*time.Millisecond)
}

func TestServerCutConnectionsKeepsTheKeys(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	rx := server.New(t)
	require.NoError(t, rx.Value[string]("k").Set(t.Context(), "v"))
	sub := rx.Redis().Subscribe(t.Context(), "events")
	defer sub.Close()
	_, err := sub.ReceiveTimeout(t.Context(), 5*time.Second)
	require.NoError(t, err)

	server.CutConnections(t)
	_, err = sub.ReceiveTimeout(t.Context(), 5*time.Second)
	require.Error(t, err, "the pub/sub connection is closed")
	value, err := rx.Value[string]("k").Get(t.Context())
	require.NoError(t, err) // go-redis sends the GET again on a new connection
	require.Equal(t, "v", value.OrElse(""))
}

func TestFaultyClientRunsTheCommandAndLosesItsReply(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	key := redistest.Namespace(t) + ":k"
	rdb, faults := redistest.FaultyClient(t, redistest.Addr(t), func(o *redis.Options) { o.MaxRetries = -1 })

	faults.LoseReply("SET")
	require.Error(t, rdb.Set(ctx, key, "v", 0).Err())
	require.Equal(t, 1, faults.Lost())
	require.Equal(t, "v", rdb.Get(ctx, key).Val(), "the server ran the command")

	require.NoError(t, rdb.Set(ctx, key, "w", 0).Err(), "only one reply is lost")
	require.Equal(t, 1, faults.Lost())
}
