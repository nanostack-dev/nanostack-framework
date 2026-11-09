package layers_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/internal/layers"
)

func TestLayer(t *testing.T) {
	for rel, want := range map[string]int{
		"core/nano": 1, "core": 1, "http/problem": 2, "pg/nanopg": 2, "redis/nanocache": 2, "otel/nanotrace": 2,
		"gen": 3, "gen/wire": 3, "gen/cmd/nano": 3, "gen/repo": 3,
		"gen/repo/internal/repotest": 0, "gen/repo/internal/repotest/db/shop/public/table": 0,
		"cmd/nano": 0, "pkg/log": 0, "modules/cache": 0, "internal/layers": 0, "coreutil": 0, "pgx": 0, "pkg/functional": 0,
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
		"pkg/functional": false, "pkg/functional/internal/gen": false, "pkg/functionalx": true,
	} {
		if got := layers.Legacy(rel); got != want {
			t.Errorf("Legacy(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestMayImport(t *testing.T) {
	for _, tt := range []struct {
		from, to string
		want     bool
	}{
		{"pg/nanopg", "pkg/db/transactor", true},
		{"otel/nanotrace", "pkg/tracing", true},
		{"http/nanoserver", "pkg/health", true},
		{"pg/nanopg", "core/nano", true},
		{"http/nanoserver", "pg/nanopg", true},
		{"pg/nanomigrate", "modules/migrations", false},
		{"redis/nanocache", "pkg/redisx", false},
		{"otel/nanotrace", "pkg/log", false},
		{"pg/nanopg", "app", false},
		{"pg/nanopg", "gen/wire", false},
		{"core/nano", "pkg/db/transactor", false},
		// pkg/functional is the leaf layer of plan section 06: every layer may
		// import it.
		{"core/nano", "pkg/functional", true},
		{"core/config", "pkg/functional", true},
		{"http/problem", "pkg/functional", true},
		{"gen/config", "pkg/functional", true},
		{"gen/cmd/nano", "pkg/functional", true},
		{"gen/cmd/nano", "core/nano", true},
		{"gen/cmd/nano", "pkg/db/transactor", false},
		{"gen/repo", "pkg/db/transactor", false},
		{"gen/repo/internal/repotest", "pkg/db/transactor", true},
		{"core/nano", "pkg/functionalx", false},
		{"core/log", "pg/nanopg", false},
		{"core/log", "otel/nanotrace", false},
		{"core/log", "redis/nanocache", false},
		{"gen/wire", "pkg/tracing", false},
		{"modules/nanofx", "core/nano", true},
	} {
		if got, _ := layers.MayImport(tt.from, tt.to); got != tt.want {
			t.Errorf("MayImport(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

// rootTrees are the go list patterns of the layered trees of the root module.
// Tooling is its own module, listed from its directory.
func rootTrees() []string {
	var patterns []string
	for _, tree := range layers.Trees {
		if tree != layers.Tooling {
			patterns = append(patterns, "./"+tree+"/...")
		}
	}
	return patterns
}

// rootDir returns the directory of the root module.
func rootDir(t *testing.T) string {
	t.Helper()
	gomod, err := exec.CommandContext(t.Context(), "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(strings.TrimSpace(string(gomod)))
}

// goCmd runs a go subcommand in dir and returns its output.
func goCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %v in %s: %v\n%s", args, dir, err, stderr.String())
	}
	return string(out)
}

// goList runs go list in the root module and returns its output.
func goList(t *testing.T, args ...string) string {
	t.Helper()
	return goCmd(t, rootDir(t), append([]string{"list"}, args...)...)
}

// goListTooling runs go list in the tooling module and returns its output.
func goListTooling(t *testing.T, args ...string) string {
	t.Helper()
	return goCmd(t, filepath.Join(rootDir(t), layers.Tooling), append([]string{"list"}, args...)...)
}

// TestNoUpwardImports fails when a package, or one of its test files,
// imports a package of a higher layer (core importing http, pg, redis, otel,
// gen or cmd, for example) or a package of a legacy tree (pkg, modules or
// app) that is not in AllowedLegacy for its layer.
func TestNoUpwardImports(t *testing.T) {
	// The integration tag lists the container tests too.
	args := []string{"-test", "-tags", "integration",
		"-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}" +
			"{{range .XTestImports}} {{.}}{{end}}"}
	out := goList(t, append(args, rootTrees()...)...) + goListTooling(t, append(args, "./...")...)
	checked := map[string]bool{}
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// A test variant is listed as "p [p.test]"; its path is the first field.
		from, ok := strings.CutPrefix(fields[0], layers.Module+"/")
		if !ok || layers.Layer(from) == 0 {
			continue
		}
		top, _, _ := strings.Cut(from, "/")
		checked[top] = true
		for _, imp := range fields[1:] {
			to, internal := strings.CutPrefix(imp, layers.Module+"/")
			if !internal {
				continue
			}
			if allowed, why := layers.MayImport(from, to); !allowed {
				t.Errorf("%s imports %s, %s", from, to, why)
			}
		}
	}
	for _, tree := range layers.Trees {
		if !checked[tree] {
			t.Errorf("go list returned no package of %s/; the test checks nothing there", tree)
		}
	}
}

// TestNoFxOrZerolog fails when a package of a runtime layer depends on Fx or
// zerolog, directly or through an allowed legacy primitive or a leaf.
func TestNoFxOrZerolog(t *testing.T) {
	banned := []string{"go.uber.org/fx", "go.uber.org/dig", "github.com/rs/zerolog"}
	patterns := []string{"./core/...", "./http/...", "./pg/...", "./redis/...", "./otel/..."}
	for _, rel := range layers.AllowedLegacy {
		patterns = append(patterns, "./"+rel)
	}
	// A leaf is its own module: go list from the root resolves it by import
	// path, through go.mod, not by directory.
	for _, rel := range layers.Leaves {
		patterns = append(patterns, layers.Module+"/"+rel)
	}
	out := goList(t, append([]string{"-deps", "-f", "{{.ImportPath}}"}, patterns...)...)
	for dep := range strings.Lines(out) {
		dep = strings.TrimSpace(dep)
		for _, b := range banned {
			if dep == b || strings.HasPrefix(dep, b+"/") {
				t.Errorf("a runtime layer or an allowed primitive depends on %s", dep)
			}
		}
	}
}

// TestToolingIsItsOwnModule fails when the generators or the nano CLI come
// back into the root module, or when the root module requires the tooling
// module or golang.org/x/tools. A runtime consumer resolves only the root
// module's graph, so the generator dependencies must stay out of it.
func TestToolingIsItsOwnModule(t *testing.T) {
	if got, want := strings.TrimSpace(goListTooling(t, "-m")), layers.Module+"/"+layers.Tooling; got != want {
		t.Fatalf("the %s directory is module %q, want %q", layers.Tooling, got, want)
	}
	for pkg := range strings.Lines(goList(t, "./...")) {
		rel, _ := strings.CutPrefix(strings.TrimSpace(pkg), layers.Module+"/")
		top, _, _ := strings.Cut(rel, "/")
		if top == layers.Tooling || top == "cmd" {
			t.Errorf("the root module lists %s; tooling belongs in the %s module", pkg, layers.Tooling)
		}
	}
	var mod struct {
		Require []struct{ Path string }
	}
	if err := json.Unmarshal([]byte(goCmd(t, rootDir(t), "mod", "edit", "-json")), &mod); err != nil {
		t.Fatal(err)
	}
	for _, req := range mod.Require {
		if req.Path == layers.Module+"/"+layers.Tooling || req.Path == "golang.org/x/tools" {
			t.Errorf("the root go.mod requires %s; only the %s module may", req.Path, layers.Tooling)
		}
	}
	// No root package, test or integration test included, builds with x/tools.
	deps := goList(t, "-test", "-tags", "integration", "-f", "{{.ImportPath}}{{range .Deps}} {{.}}{{end}}", "./...")
	for line := range strings.Lines(deps) {
		pkg, list, _ := strings.Cut(strings.TrimSpace(line), " ")
		for dep := range strings.FieldsSeq(list) {
			if strings.HasPrefix(dep, "golang.org/x/tools/") {
				t.Errorf("%s depends on %s; move the code that needs golang.org/x/tools into the %s module",
					pkg, dep, layers.Tooling)
				break
			}
		}
	}
}
