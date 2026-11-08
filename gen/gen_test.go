package gen_test

import (
	"bytes"
	"errors"
	"fmt"
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

func TestSyncFileErrorIsTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	_, err := gen.Sync("demo", []gen.File{{Path: dir, Data: []byte("x")}}, true)
	if err == nil || errors.Is(err, gen.ErrStale) || !errors.Is(err, gen.ErrEnvironment) {
		t.Fatalf("Sync on a directory = %v, want a read error that wraps ErrEnvironment", err)
	}
	if msg := err.Error(); strings.Count(msg, dir) != 1 || strings.Contains(msg, "nano gen") ||
		!strings.Contains(msg, "\n  fix: ") {
		t.Fatalf("Sync error = %q, want the path once, a fix and no generator prefix", msg)
	}
	if code := gen.ExitCode(err); code != 3 {
		t.Fatalf("ExitCode = %d, want 3", code)
	}
}

func TestErrorFormats(t *testing.T) {
	for _, tc := range []struct {
		err  *gen.Error
		want string
	}{
		{gen.Errorf("a.go", 3, "do this", "bad %s", "thing"), "a.go:3: bad thing\n  fix: do this"},
		{gen.Errorf("openapi.yaml", 0, "do this", "bad"), "openapi.yaml: bad\n  fix: do this"},
		{gen.Errorf("", 0, "do this", "bad"), "bad\n  fix: do this"},
		{&gen.Error{Msg: "bad"}, "bad"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
}

func TestReport(t *testing.T) {
	stale := &gen.StaleError{Name: "demo", Files: []string{"a.gen.go", "b.gen.go"}}
	cases := map[string]struct {
		err  error
		code int
		want string
	}{
		"nothing": {nil, 0, ""},
		"located and unlocated": {
			errors.Join(gen.Errorf("a.go", 3, "fix a", "bad a"), gen.Errorf("", 0, "fix b", "bad b")),
			1,
			"a.go:3: bad a\n  fix: fix a\nnano gen demo: bad b\n  fix: fix b\n",
		},
		"stale": {stale, 1, "nano gen demo: generated files are stale: a.gen.go, b.gen.go\n  fix: run nano gen demo\n"},
		"environment wins": {
			errors.Join(gen.Errorf("a.go", 3, "fix a", "bad a"), gen.EnvErrorf("start it", "no daemon")),
			3,
			"a.go:3: bad a\n  fix: fix a\nnano gen demo: no daemon\n  fix: start it\n",
		},
		"an error without a fix is a bug": {
			errors.New("plain"),
			1,
			"nano gen demo: plain\n  fix: none known; an error without a fix is a nano gen bug, report it\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if code := gen.Report(&out, "demo", tc.err); code != tc.code || out.String() != tc.want {
				t.Fatalf("Report = %d, %q; want %d, %q", code, out.String(), tc.code, tc.want)
			}
		})
	}
}

func TestErrorsFlattensJoins(t *testing.T) {
	a, b, c := gen.Errorf("a", 1, "f", "a"), gen.Errorf("b", 2, "f", "b"), gen.Errorf("c", 3, "f", "c")
	got := gen.Errors(errors.Join(a, errors.Join(b, nil, c)))
	if !slices.Equal(got, []*gen.Error{a, b, c}) {
		t.Fatalf("Errors = %v, want a, b, c in order", got)
	}
	if gen.Errors(nil) != nil {
		t.Fatal("Errors(nil) is not empty")
	}
	wrapped := fmt.Errorf("context: %w", a)
	if got = gen.Errors(wrapped); len(got) != 1 || got[0].Fix != "" || got[0].Msg != "context: a:1: a\n  fix: f" {
		t.Fatalf("Errors(wrapped) = %+v, want one problem without a fix that keeps the wrapper's text", got)
	}
}
