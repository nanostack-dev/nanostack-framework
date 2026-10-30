//go:build integration

package redistest_test

import (
	"strings"
	"testing"

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
