package wire

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// TestClashOutputIgnoresTheProviderOrder generates clashthree from its
// manifest with the providers reversed and expects the same bytes, so
// reordering a manifest renames no field or variable of a clashing type.
func TestClashOutputIgnoresTheProviderOrder(t *testing.T) {
	opts := caseOptions(t, "clashthree", "")
	manifest := filepath.Join(opts.Dir, "nano.go")
	src, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	const order = "\t\tus.NewLedger,\n\t\torders.NewLedger,\n\t\tcache.NewLedger,\n\t\teu.NewLedger,\n"
	const reversed = "\t\teu.NewLedger,\n\t\tcache.NewLedger,\n\t\torders.NewLedger,\n\t\tus.NewLedger,\n"
	if !strings.Contains(string(src), order) {
		t.Fatalf("%s no longer lists the providers this test reverses", manifest)
	}
	opts.Overlay = map[string][]byte{manifest: []byte(strings.Replace(string(src), order, reversed, 1))}
	got := generate(t, opts)
	want, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != string(want) {
		t.Fatalf("reversed providers give another file:\n%s", got.Data)
	}
}

// TestCheckAfterTheClashRename runs -check on output that an older nano wrote
// with numbered fields: -check fails as stale, a run rewrites it with the
// path-qualified names, and -check passes again.
func TestCheckAfterTheClashRename(t *testing.T) {
	committed := caseOptions(t, "clashtwo", "")
	committed.Check = true
	if err := Run(committed); err != nil {
		t.Fatalf("Check on the committed output = %v", err)
	}

	opts := caseOptions(t, "clashtwo", "internal/gen/stale")
	opts.Check = true
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(opts.Dir, "internal/gen/stale")) })
	res := generate(t, opts)
	old := strings.NewReplacer(
		"BillingRepositoryRepository", "RepositoryRepository",
		"InvoiceRepositoryRepository", "RepositoryRepository2",
	).Replace(string(res.Data))
	if old == string(res.Data) {
		t.Fatal("the generated file holds neither path-qualified field")
	}
	res.Data = []byte(old)
	if _, err := gen.Sync("wire", []gen.File{res}, false); err != nil {
		t.Fatal(err)
	}
	if err := Run(opts); !errors.Is(err, gen.ErrStale) {
		t.Fatalf("Check on numbered fields = %v, want gen.ErrStale", err)
	}
	opts.Check = false
	if err := Run(opts); err != nil {
		t.Fatalf("Run = %v", err)
	}
	opts.Check = true
	if err := Run(opts); err != nil {
		t.Fatalf("Check after Run rewrote the file = %v", err)
	}
}
