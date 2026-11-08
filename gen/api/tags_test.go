package genapi

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
)

var update = flag.Bool("update", false, "rewrite the golden files of the tag split")

// serverDir returns a directory for a server package named api.
func serverDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "api")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The tag rules: each spec under testdata/tags breaks one, and
// errors.golden holds what nano gen api reports, at the spec's lines.
func TestTagRules(t *testing.T) {
	for _, name := range []string{
		"untagged", "multitagged", "undeclared", "unused", "collision", "noname", "duplicate", "schemaclash",
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("testdata", "tags", name)
			files, err := Generate(Options{Spec: filepath.Join(dir, "openapi.yaml"),
				Server: project.Package{Dir: serverDir(t)}})
			if files != nil {
				t.Errorf("files = %v, want none", paths(files))
			}
			for _, p := range gentest.Problems(t, err) {
				if p.File == "" {
					t.Errorf("problem %q names no file", p.Msg)
				}
			}
			if gentest.Environment(err) {
				t.Errorf("a spec problem is classed as the environment: %v", err)
			}
			golden(t, filepath.Join(dir, "errors.golden"), []byte(err.Error()+"\n"))
		})
	}
}

// The tag split of a spec with a summary, a deprecated operation, a path
// alias, an operation without operationId, a tag with a space, one with an
// initialism, one that is a Go keyword in lower case and one that is the
// name of the nil check of Handlers in lower case.
func TestHandlersGolden(t *testing.T) {
	dir := filepath.Join("testdata", "tags", "split")
	files, err := Generate(Options{Spec: filepath.Join(dir, "openapi.yaml"), Server: project.Package{Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || filepath.Base(files[1].Path) != HandlersFile {
		t.Fatalf("files = %v", paths(files))
	}
	golden(t, filepath.Join(dir, HandlersFile+".golden"), files[1].Data)
}

// oapi-codegen keeps its options in package state, which every Generate
// sets. handlers.gen.go reads the operations under the server's options,
// so a client run, before, after or at the same time, does not change it.
func TestHandlersDoNotDependOnClientRuns(t *testing.T) {
	spec := filepath.Join("testdata", "tags", "split", "openapi.yaml")
	client := project.Client{Dir: filepath.Join(t.TempDir(), "client"), Package: "client", PlainTypes: true}
	clientRun := func() {
		if _, err := Generate(Options{Spec: spec, Clients: []project.Client{client}}); err != nil {
			t.Error(err)
		}
	}
	handlers := func(opts Options) []byte {
		t.Helper()
		files, err := Generate(opts)
		if err != nil {
			t.Error(err)
			return nil
		}
		for _, f := range files {
			if filepath.Base(f.Path) == HandlersFile {
				return f.Data
			}
		}
		t.Errorf("no %s in %v", HandlersFile, paths(files))
		return nil
	}
	server := project.Package{Dir: serverDir(t)}
	alone := handlers(Options{Spec: spec, Server: server})

	withClient := Options{Spec: spec, Server: server, Clients: []project.Client{client}}
	if got := handlers(withClient); !bytes.Equal(got, alone) {
		t.Errorf("a client run after the server changed handlers.gen.go:\n%s", firstDiff(got, alone))
	}
	clientRun()
	if got := handlers(Options{Spec: spec, Server: server}); !bytes.Equal(got, alone) {
		t.Errorf("a client run before the server changed handlers.gen.go:\n%s", firstDiff(got, alone))
	}

	var wg sync.WaitGroup
	results := make([][]byte, 8)
	for i := range results {
		wg.Go(func() {
			if i%2 == 1 {
				clientRun()
				return
			}
			results[i] = handlers(Options{Spec: spec, Server: server})
		})
	}
	wg.Wait()
	for i := 0; i < len(results); i += 2 {
		if !bytes.Equal(results[i], alone) {
			t.Errorf("run %d beside client runs changed handlers.gen.go:\n%s", i, firstDiff(results[i], alone))
		}
	}
}

// A client-only run checks no tag rule: a client module may consume a spec
// whose server lives elsewhere.
func TestClientIgnoresTagRules(t *testing.T) {
	spec := filepath.Join("testdata", "tags", "untagged", "openapi.yaml")
	if _, err := Generate(Options{Spec: spec,
		Clients: []project.Client{{Dir: t.TempDir(), Package: "client"}}}); err != nil {
		t.Errorf("Generate = %v, want no error", err)
	}
}

func TestFieldName(t *testing.T) {
	for in, want := range map[string]string{
		"Email":       "email",
		"StatusPages": "statusPages",
		"APIKeys":     "apiKeys",
		"API":         "api",
		"OAuth":       "oAuth",
		"V2Users":     "v2Users",
		"Type":        "type_",
		"Func":        "func_",
		"Checked":     "checked_",
	} {
		if got := fieldName(in); got != want {
			t.Errorf("fieldName(%q) = %q, want %q", in, got, want)
		}
	}
}

// golden compares got with the file at path, or rewrites it with -update.
func golden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go -C gen test ./api -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is stale; run go -C gen test ./api -update and review the diff\n%s", path, firstDiff(got, want))
	}
}
