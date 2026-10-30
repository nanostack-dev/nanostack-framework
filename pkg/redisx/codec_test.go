package redisx_test

import (
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
)

type session struct {
	User  string   `json:"user"`
	Roles []string `json:"roles"`
}

func TestJSONRoundTrips(t *testing.T) {
	codec := redisx.JSON[session]()
	raw, err := codec.Encode(session{User: "ana", Roles: []string{"admin"}})
	require.NoError(t, err)
	require.JSONEq(t, `{"user":"ana","roles":["admin"]}`, raw)

	decoded, err := codec.Decode(raw)
	require.NoError(t, err)
	require.Equal(t, session{User: "ana", Roles: []string{"admin"}}, decoded)
}

func TestJSONReportsWhatItCannotDecode(t *testing.T) {
	_, err := redisx.JSON[session]().Decode("not json")
	require.ErrorContains(t, err, "decode redisx_test.session")
}

func TestJSONReportsWhatItCannotEncode(t *testing.T) {
	_, err := redisx.JSON[chan int]().Encode(make(chan int))
	require.ErrorContains(t, err, "encode chan int")
}

func TestTextKeepsStringsAsTheyAre(t *testing.T) {
	raw, err := redisx.Text().Encode(`"quoted"`)
	require.NoError(t, err)
	require.Equal(t, `"quoted"`, raw)
}

func TestKeyAddsThePrefix(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{})
	t.Cleanup(func() { _ = rdb.Close() })

	require.Equal(t, "orders", redisx.New(rdb).Key("orders"))
	require.Equal(t, "app:orders", redisx.New(rdb, redisx.WithPrefix("app")).Key("orders"))
	require.Equal(t, "app:orders", redisx.New(rdb, redisx.WithPrefix("app:")).Key("orders"))
}

func TestStructuresUseTheFullKeyName(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{})
	t.Cleanup(func() { _ = rdb.Close() })
	rx := redisx.New(rdb, redisx.WithPrefix("app"))

	require.Equal(t, "app:a", rx.Value[string]("a").Key())
	require.Equal(t, "app:b", rx.Map[int]("b").Key())
	require.Equal(t, "app:c", rx.Set[string]("c").Key())
	require.Equal(t, "app:d", rx.Queue[session]("d").Key())
	require.Equal(t, "app:e", rx.Counter("e").Key())
	require.Equal(t, "app:f", rx.Topic[session]("f").Channel())
}
