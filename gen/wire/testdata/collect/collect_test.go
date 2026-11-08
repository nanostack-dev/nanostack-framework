package collect_test

import (
	"slices"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/internal/gen/wire"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/syncs"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/workflow"
)

func names(defs []*workflow.Definition) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

func TestCollectionsAndComponents(t *testing.T) {
	app, c, err := wire.BuildComponents(t.Context(), wire.Config{}, wire.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(t.Context()) })

	// Two members of one type, from two modules, in declaration order.
	if got := c.Enriched.Keys(t.Context()); !slices.Equal(got, []string{"request_id", "principal"}) {
		t.Fatalf("enricher keys = %q, want request_id then principal", got)
	}
	if got := names(c.Worker.Defs); !slices.Equal(got, []string{"sync", "billing"}) {
		t.Fatalf("worker definitions = %q, want sync then billing", got)
	}
	if c.Worker.Crons == nil || len(c.Worker.Crons) != 0 {
		t.Fatalf("crons = %#v, want an empty, non-nil collection", c.Worker.Crons)
	}
	// fx.Populate: the suite reaches shared values directly.
	if c.Repository == nil || c.SyncService.Repo != c.Repository || c.Service.Repo != c.Repository {
		t.Fatal("Components must expose the one *auth.Repository every consumer shares")
	}
}

func TestOverridingACollectionSkipsItsMembers(t *testing.T) {
	before := syncs.Built.Load()
	fake := []*workflow.Definition{{Name: "fake"}}
	app, c, err := wire.BuildComponents(t.Context(), wire.Config{}, wire.Overrides{Definitions: fake})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(t.Context()) })
	if got := syncs.Built.Load() - before; got != 0 {
		t.Fatalf("ProvideSyncDefinition ran %d times; the override must skip it", got)
	}
	if c.SyncService != nil {
		t.Fatal("SyncService was built, but only the overridden collection needed it")
	}
	if got := names(c.Worker.Defs); !slices.Equal(got, []string{"fake"}) {
		t.Fatalf("worker definitions = %q, want the override", got)
	}
}
