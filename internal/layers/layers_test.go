package layers_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/internal/layers"
)

func TestLayer(t *testing.T) {
	for rel, want := range map[string]int{
		"core/nano": 1, "core": 1, "http/problem": 2, "gen": 3, "gen/wire": 3, "cmd/nano": 3,
		"pkg/log": 0, "modules/cache": 0, "internal/layers": 0, "coreutil": 0,
	} {
		if got := layers.Layer(rel); got != want {
			t.Errorf("Layer(%q) = %d, want %d", rel, got, want)
		}
	}
}

func TestLegacy(t *testing.T) {
	for rel, want := range map[string]bool{
		"pkg/fault": true, "modules/cache": true, "app": true, "pkg": true,
		"core/nano": false, "http/problem": false, "gen/wire": false, "internal/layers": false, "application": false,
	} {
		if got := layers.Legacy(rel); got != want {
			t.Errorf("Legacy(%q) = %v, want %v", rel, got, want)
		}
	}
}

// TestNoUpwardImports fails when a package, or one of its test files,
// imports a package of a higher layer (core importing http, gen or cmd, or
// http importing gen or cmd) or a package of a legacy tree (pkg, modules or
// app).
func TestNoUpwardImports(t *testing.T) {
	gomod, err := exec.CommandContext(t.Context(), "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatal(err)
	}
	list := exec.CommandContext(t.Context(), "go", "list", "-test",
		"-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}"+
			"{{range .XTestImports}} {{.}}{{end}}",
		"./core/...", "./http/...", "./gen/...", "./cmd/...")
	list.Dir = filepath.Dir(strings.TrimSpace(string(gomod)))
	var stderr bytes.Buffer
	list.Stderr = &stderr
	out, err := list.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	checked := 0
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// A test variant is listed as "p [p.test]"; its path is the first field.
		from, ok := strings.CutPrefix(fields[0], layers.Module+"/")
		if !ok || layers.Layer(from) == 0 {
			continue
		}
		checked++
		for _, imp := range fields[1:] {
			to, internal := strings.CutPrefix(imp, layers.Module+"/")
			switch {
			case !internal:
			case layers.Legacy(to):
				t.Errorf("%s imports %s, a legacy package; move what it needs into the layers", from, to)
			case layers.Layer(to) > layers.Layer(from):
				t.Errorf("%s imports %s, a higher layer", from, to)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("go list returned no framework packages:\n%s", out)
	}
}
