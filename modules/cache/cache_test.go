package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/nanostack-dev/nanostack-framework/modules/cache"
)

type product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// fakeStore is an in-memory Store mirroring RedisCache's semantics: a miss is
// ErrCacheKeyNotFound, and GetOrElse stores whatever the fallback returns.
// Embedding NoOpCache supplies the parts of Store that Cache never touches.
type fakeStore struct {
	*cache.NoOpCache

	data        map[string]string
	ttls        map[string]time.Duration
	evicted     []string
	patterns    []string
	setFailsFor string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		NoOpCache: cache.NewNoOpCache(),
		data:      map[string]string{},
		ttls:      map[string]time.Duration{},
	}
}

func (f *fakeStore) Get(_ context.Context, key string) (string, error) {
	raw, ok := f.data[key]
	if !ok {
		return "", cache.ErrCacheKeyNotFound
	}
	return raw, nil
}

func (f *fakeStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if key == f.setFailsFor {
		return errors.New("redis down")
	}
	f.data[key] = value
	f.ttls[key] = ttl
	return nil
}

func (f *fakeStore) GetOrElse(
	ctx context.Context, key string, fallback func() (string, error), ttl time.Duration,
) (string, error) {
	if raw, err := f.Get(ctx, key); err == nil {
		return raw, nil
	} else if !errors.Is(err, cache.ErrCacheKeyNotFound) {
		return "", err
	}
	value, err := fallback()
	if err != nil {
		return "", err
	}
	if setErr := f.Set(ctx, key, value, ttl); setErr != nil {
		return value, setErr
	}
	return value, nil
}

func (f *fakeStore) Evict(_ context.Context, key string) error {
	f.evicted = append(f.evicted, key)
	delete(f.data, key)
	return nil
}

func (f *fakeStore) EvictPattern(_ context.Context, pattern string) error {
	f.patterns = append(f.patterns, pattern)
	return nil
}

func (f *fakeStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := f.data[key]
	return ok, nil
}

func newProductCache(s cache.Store) cache.Cache[product] {
	return cache.New[product](s, "product", 30*time.Minute, zerolog.Nop())
}

func TestCacheKeyNamespacing(t *testing.T) {
	products := newProductCache(newFakeStore())

	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "no parts is the bare prefix", parts: nil, want: "product"},
		{name: "one part", parts: []string{"tenant1"}, want: "product:tenant1"},
		{name: "several parts join with colons", parts: []string{"tenant1", "prod9"}, want: "product:tenant1:prod9"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := products.Key(tc.parts...).String(); got != tc.want {
				t.Fatalf("Key(%v) = %q, want %q", tc.parts, got, tc.want)
			}
		})
	}
}

func TestCacheGetAndSet(t *testing.T) {
	store := newFakeStore()
	entry := newProductCache(store).Key("tenant1", "prod9")

	t.Run("miss reports ErrCacheKeyNotFound", func(t *testing.T) {
		got, err := entry.Get(context.Background())
		if !errors.Is(err, cache.ErrCacheKeyNotFound) {
			t.Fatalf("Get() err = %v, want ErrCacheKeyNotFound", err)
		}
		if got != nil {
			t.Fatalf("Get() = %v, want nil on miss", got)
		}
	})

	t.Run("set then get round-trips the value", func(t *testing.T) {
		if err := entry.Set(context.Background(), &product{ID: "prod9", Name: "Anchor"}); err != nil {
			t.Fatalf("Set() err = %v", err)
		}
		got, err := entry.Get(context.Background())
		if err != nil {
			t.Fatalf("Get() err = %v", err)
		}
		if got == nil || got.ID != "prod9" || got.Name != "Anchor" {
			t.Fatalf("Get() = %+v, want the stored product", got)
		}
	})

	t.Run("set applies the namespace TTL", func(t *testing.T) {
		if got := store.ttls["product:tenant1:prod9"]; got != 30*time.Minute {
			t.Fatalf("stored TTL = %v, want 30m", got)
		}
	})

	t.Run("exists tracks presence", func(t *testing.T) {
		found, err := entry.Exists(context.Background())
		if err != nil || !found {
			t.Fatalf("Exists() = (%v, %v), want (true, nil)", found, err)
		}
	})
}

func TestCacheGetOrElse(t *testing.T) {
	t.Run("miss loads, caches, and returns", func(t *testing.T) {
		entry := newProductCache(newFakeStore()).Key("tenant1", "prod9")
		calls := 0
		load := func() (*product, error) {
			calls++
			return &product{ID: "prod9", Name: "Anchor"}, nil
		}

		got, err := entry.GetOrElse(context.Background(), load)
		if err != nil || got == nil || got.Name != "Anchor" {
			t.Fatalf("GetOrElse() = (%+v, %v), want the loaded product", got, err)
		}

		got, err = entry.GetOrElse(context.Background(), load)
		if err != nil || got == nil || got.Name != "Anchor" {
			t.Fatalf("second GetOrElse() = (%+v, %v)", got, err)
		}
		if calls != 1 {
			t.Fatalf("load called %d times, want 1 — second read should hit the cache", calls)
		}
	})

	t.Run("nil from load means absent, and caches nothing", func(t *testing.T) {
		store := newFakeStore()
		entry := newProductCache(store).Key("tenant1", "missing")

		//nolint:nilnil // exercises the documented "nil means absent" loader contract
		absent := func() (*product, error) { return nil, nil }
		got, err := entry.GetOrElse(context.Background(), absent)
		if err != nil {
			t.Fatalf("GetOrElse() err = %v, want nil — absence is not a failure", err)
		}
		if got != nil {
			t.Fatalf("GetOrElse() = %+v, want nil", got)
		}
		if len(store.data) != 0 {
			t.Fatalf("cache holds %v, want nothing stored for an absent record", store.data)
		}
	})

	t.Run("load error propagates", func(t *testing.T) {
		entry := newProductCache(newFakeStore()).Key("tenant1", "prod9")
		sentinel := errors.New("db down")

		got, err := entry.GetOrElse(context.Background(), func() (*product, error) { return nil, sentinel })
		if !errors.Is(err, sentinel) {
			t.Fatalf("GetOrElse() err = %v, want %v", err, sentinel)
		}
		if got != nil {
			t.Fatalf("GetOrElse() = %+v, want nil on load failure", got)
		}
	})

	t.Run("cache write failure surfaces rather than being swallowed", func(t *testing.T) {
		store := newFakeStore()
		store.setFailsFor = "product:tenant1:prod9"
		entry := newProductCache(store).Key("tenant1", "prod9")

		_, err := entry.GetOrElse(context.Background(), func() (*product, error) {
			return &product{ID: "prod9"}, nil
		})
		if err == nil {
			t.Fatal("GetOrElse() err = nil, want the cache write failure")
		}
	})
}

func TestCacheEviction(t *testing.T) {
	store := newFakeStore()
	products := newProductCache(store)

	t.Run("evict drops the addressed key", func(t *testing.T) {
		entry := products.Key("tenant1", "prod9")
		if err := entry.Set(context.Background(), &product{ID: "prod9"}); err != nil {
			t.Fatalf("Set() err = %v", err)
		}
		if err := entry.Evict(context.Background()); err != nil {
			t.Fatalf("Evict() err = %v", err)
		}
		if len(store.evicted) != 1 || store.evicted[0] != "product:tenant1:prod9" {
			t.Fatalf("evicted %v, want [product:tenant1:prod9]", store.evicted)
		}
	})

	t.Run("evict prefix scopes the pattern to the given parts", func(t *testing.T) {
		if err := products.EvictPrefix(context.Background(), "tenant1"); err != nil {
			t.Fatalf("EvictPrefix() err = %v", err)
		}
		if len(store.patterns) != 1 || store.patterns[0] != "product:tenant1:*" {
			t.Fatalf("patterns = %v, want [product:tenant1:*]", store.patterns)
		}
	})

	t.Run("evict prefix with no parts clears the whole namespace", func(t *testing.T) {
		store.patterns = nil
		if err := products.EvictPrefix(context.Background()); err != nil {
			t.Fatalf("EvictPrefix() err = %v", err)
		}
		if len(store.patterns) != 1 || store.patterns[0] != "product:*" {
			t.Fatalf("patterns = %v, want [product:*]", store.patterns)
		}
	})
}

// TestCacheNamespacesAreIndependent guards the reason Cache[T] exists: two
// caches over different types cannot collide, even on identical key parts.
func TestCacheNamespacesAreIndependent(t *testing.T) {
	store := newFakeStore()
	products := cache.New[product](store, "product", time.Minute, zerolog.Nop())
	apiKeys := cache.New[string](store, "apikey", time.Minute, zerolog.Nop())

	if err := products.Key("same").Set(context.Background(), &product{ID: "p"}); err != nil {
		t.Fatalf("Set() err = %v", err)
	}
	got, err := apiKeys.Key("same").Get(context.Background())
	if !errors.Is(err, cache.ErrCacheKeyNotFound) {
		t.Fatalf("apiKeys Get() = (%v, %v), want ErrCacheKeyNotFound — namespaces must not collide", got, err)
	}
}
