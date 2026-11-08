package gen_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

func TestSync(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "same.go")
	if err := os.WriteFile(same, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "sub", "new.go")
	files := []gen.File{{Path: same, Data: []byte("same")}, {Path: fresh, Data: []byte("new")}}

	_, err := gen.Sync("demo", files, true)
	stale, ok := errors.AsType[*gen.StaleError](err)
	if !ok || !errors.Is(err, gen.ErrStale) || !slices.Equal(stale.Files, []string{fresh}) ||
		stale.Name != "demo" || !strings.Contains(err.Error(), "run nano gen demo") {
		t.Fatalf("check = %v, want a *StaleError naming only %s", err, fresh)
	}
	if _, statErr := os.Stat(fresh); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("check wrote %s", fresh)
	}

	written, err := gen.Sync("demo", files, false)
	if err != nil || !slices.Equal(written, []string{fresh}) {
		t.Fatalf("write = %q, %v; want only %s", written, err, fresh)
	}
	if _, err = gen.Sync("demo", files, true); err != nil {
		t.Fatalf("check after write = %v", err)
	}
}

func TestSyncFileErrorNamesThePathOnce(t *testing.T) {
	dir := t.TempDir()
	_, err := gen.Sync("demo", []gen.File{{Path: dir, Data: []byte("x")}}, true)
	if err == nil || errors.Is(err, gen.ErrStale) {
		t.Fatalf("Sync on a directory = %v, want a read error", err)
	}
	if msg := err.Error(); strings.Count(msg, dir) != 1 || strings.Contains(msg, "nano gen") {
		t.Fatalf("Sync error = %q, want the path once and no generator prefix", msg)
	}
}
