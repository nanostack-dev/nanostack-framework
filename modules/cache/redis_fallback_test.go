package cache

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisCommandHook struct {
	process redis.ProcessHook
}

func (h redisCommandHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h redisCommandHook) ProcessHook(redis.ProcessHook) redis.ProcessHook { return h.process }

func (h redisCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestRedisFallbackPolicy(t *testing.T) {
	readErr := errors.New("read unavailable")
	loadErr := errors.New("loader failed")
	writeErr := errors.New("write unavailable")
	for _, computedExpiry := range []bool{false, true} {
		for _, tt := range []struct {
			name     string
			getErr   error
			loadErr  error
			setErr   error
			want     string
			wantErr  error
			wantLoad int
			wantSet  int
		}{
			{name: "hit", want: "cached"},
			{name: "miss", getErr: redis.Nil, want: "loaded", wantLoad: 1, wantSet: 1},
			{name: "read error", getErr: readErr, wantErr: readErr},
			{name: "loader error", getErr: redis.Nil, loadErr: loadErr, wantErr: loadErr, wantLoad: 1},
			{name: "write error retains value", getErr: redis.Nil, setErr: writeErr, want: "loaded", wantErr: writeErr, wantLoad: 1, wantSet: 1},
		} {
			variant := "fixed/"
			if computedExpiry {
				variant = "computed/"
			}
			t.Run(variant+tt.name, func(t *testing.T) {
				client := redis.NewClient(&redis.Options{Addr: "unused"})
				t.Cleanup(func() { _ = client.Close() })
				sets := 0
				client.AddHook(redisCommandHook{process: func(_ context.Context, cmd redis.Cmder) error {
					switch command := cmd.(type) {
					case *redis.StringCmd:
						command.SetVal("cached")
						command.SetErr(tt.getErr)
						return tt.getErr
					case *redis.StatusCmd:
						sets++
						if want := []any{"set", "key", "loaded", "ex", int64(60)}; !reflect.DeepEqual(command.Args(), want) {
							t.Fatalf("write args = %#v, want %#v", command.Args(), want)
						}
						command.SetErr(tt.setErr)
						return tt.setErr
					default:
						t.Fatalf("unexpected command %T", cmd)
						return nil
					}
				}})
				store := &RedisCache{client: client}
				loads := 0
				load := func() (string, error) {
					loads++
					return "loaded", tt.loadErr
				}
				var got string
				var err error
				if computedExpiry {
					got, err = store.GetOrElseWithExpiry(context.Background(), "key", func() (string, time.Duration, error) {
						value, loadErr := load()
						return value, time.Minute, loadErr
					})
				} else {
					got, err = store.GetOrElse(context.Background(), "key", load, time.Minute)
				}
				if got != tt.want || !errors.Is(err, tt.wantErr) || loads != tt.wantLoad || sets != tt.wantSet {
					t.Fatalf("got (%q, %v), loads=%d writes=%d; want (%q, %v), loads=%d writes=%d", got, err, loads, sets, tt.want, tt.wantErr, tt.wantLoad, tt.wantSet)
				}
			})
		}
	}
}
