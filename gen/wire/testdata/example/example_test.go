package example_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/gen/wire"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/greeter"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/httpapi"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/store"
)

// fakeStore is a test double. It is also a Stopper, so the App manages it.
type fakeStore struct {
	names   []string
	stopped bool
}

func (f *fakeStore) Put(_ context.Context, name string) error {
	f.names = append(f.names, name)
	return nil
}
func (f *fakeStore) Names(context.Context) ([]string, error) { return slices.Clone(f.names), nil }
func (f *fakeStore) Stop(context.Context) error              { f.stopped = true; return nil }

func config() example.Config {
	return example.Config{
		HTTP:     httpapi.Config{Addr: "127.0.0.1:0"},
		Greeting: greeter.Config{Greeting: "Hi"},
	}
}

func TestOverrideReplacesStoreAndSkipsItsProvider(t *testing.T) {
	fake := &fakeStore{}
	before, startedBefore := store.Opened.Load(), store.Started.Load()
	app, err := wire.Build(t.Context(), config(), wire.Overrides{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:  fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Opened.Load() - before; got != 0 {
		t.Fatalf("store.NewMemory ran %d times; the Store override must skip it", got)
	}
	if !slices.Equal(fake.names, []string{"ada", "grace"}) {
		t.Fatalf("Seed wrote %q to the override, want ada and grace", fake.names)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := store.Started.Load() - startedBefore; got != 0 {
		t.Fatalf("store.Memory started %d times; the Store override must prune the Starter", got)
	}
	if err := app.Ready(t.Context()); err != nil {
		t.Fatalf("Ready = %v", err)
	}
	if err := app.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !fake.stopped {
		t.Fatal("the App did not stop the overriding Store")
	}
}

func TestBuildFailureReleasesConstructedValues(t *testing.T) {
	cfg := config()
	cfg.Greeting.Greeting = "" // greeter.NewService fails after store.NewMemory ran
	var logs strings.Builder
	_, err := wire.Build(t.Context(), cfg, wire.Overrides{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err == nil || !strings.Contains(err.Error(), "nano: provide *greeter.Service: greeter.NewService: greeter: empty greeting") {
		t.Fatalf("Build = %v, want the wrapped provider error", err)
	}
	if !strings.Contains(logs.String(), "store closed") {
		t.Fatalf("the store cleanup did not run after the failed Build; logs:\n%s", logs.String())
	}
}
