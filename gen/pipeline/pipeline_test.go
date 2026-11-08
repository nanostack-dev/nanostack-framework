package pipeline

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture is a module with every step the pipeline runs: an API with its
// oapi-codegen package (validate), an error catalogue (errors) and a
// manifest with its Config (config, wire). Its committed outputs are fresh.
const fixture = "testdata/app"

// copyFixture copies the fixture module to a temporary directory and
// points its replace directives at this repository.
func copyFixture(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	err = filepath.WalkDir(fixture, func(path string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(fixture, path)
		target := filepath.Join(dst, rel)
		if e.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if rel == "go.mod" {
			data = bytes.ReplaceAll(data, []byte("=> ../../../.."), []byte("=> "+repo))
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// edit replaces old with replacement in the file at rel under dir.
func edit(t *testing.T, dir, rel, old, replacement string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(src), old, replacement, 1)
	if edited == string(src) {
		t.Fatalf("%s has no %q; update this test", rel, old)
	}
	write(t, dir, rel, edited)
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// nano runs `nano gen` with args on dir.
func nano(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Main(append([]string{"-dir", dir}, args...), &out, &errOut)
	return out.String(), errOut.String(), code
}

func wantRun(t *testing.T, name string, code, wantCode int, stdout, wantStdout, stderr, wantStderr string) {
	t.Helper()
	if code != wantCode || stdout != wantStdout || stderr != wantStderr {
		t.Fatalf("%s: exit %d, want %d\nstdout:\n%s\nwant:\n%s\nstderr:\n%s\nwant:\n%s",
			name, code, wantCode, stdout, wantStdout, stderr, wantStderr)
	}
}

// The committed fixture is fresh: -check prints nothing and exits 0.
func TestFreshFixtureChecksClean(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := nano(t, fixture, "-check")
	wantRun(t, "nano gen -check", code, 0, stdout, "", stderr, "")
}

// A renamed Config field makes config and wire stale. -check lists them and
// writes nothing; a run regenerates both, config first, so wire loads the
// fresh config.gen.go; then -check passes.
func TestStaleConfigAndWire(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "cmd/app/config.go", "Greeting GreetingConfig `config:\"greeting\"`",
		"Welcome GreetingConfig `config:\"welcome\"`")

	stdout, stderr, code := nano(t, dir, "-check")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr, `stale cmd/app/config.gen.go
stale cmd/app/CONFIG.md
stale cmd/app/.env.example
stale cmd/app/wire.gen.go
  fix: run nano gen without -check, then commit the result
`)

	stdout, stderr, code = nano(t, dir)
	wantRun(t, "nano gen", code, 0, stdout, `wrote cmd/app/config.gen.go
wrote cmd/app/CONFIG.md
wrote cmd/app/.env.example
wrote cmd/app/wire.gen.go
`, stderr, "")

	stdout, stderr, code = nano(t, dir, "-check")
	wantRun(t, "nano gen -check after the run", code, 0, stdout, "", stderr, "")
}

// -diff shows where a stale file differs.
func TestCheckDiff(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "cmd/app/config.go", `default:"hello"`, `default:"hi"`)
	_, stderr, code := nano(t, dir, "-check", "-diff", "-only", "config")
	if code != 1 || !strings.Contains(stderr, "stale cmd/app/CONFIG.md\n    @@ -") ||
		!strings.Contains(stderr, "\n    +") || !strings.Contains(stderr, "hi") {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
}

// Deleting a catalogue leaves its outputs behind with a nano gen errors
// header: -check reports them as orphans, a run deletes them.
func TestOrphanAfterDeletedCatalogue(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := os.Remove(filepath.Join(dir, "greeterr", "errors.go")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := nano(t, dir, "-check", "-only", "errors")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr, `orphan greeterr/ERRORS.md
orphan greeterr/errors.gen.go
  fix: run nano gen without -check, then commit the result
`)

	stdout, stderr, code = nano(t, dir, "-only", "errors")
	wantRun(t, "nano gen", code, 0, stdout, "deleted greeterr/ERRORS.md\ndeleted greeterr/errors.gen.go\n", stderr, "")
	if _, err := os.Stat(filepath.Join(dir, "greeterr", "errors.gen.go")); !os.IsNotExist(err) {
		t.Fatalf("errors.gen.go still exists: %v", err)
	}
}

// A step that did not run reports no orphan: -only validate keeps the
// catalogue's outputs.
func TestNoOrphanForAStepThatDidNotRun(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := os.Remove(filepath.Join(dir, "greeterr", "errors.go")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := nano(t, dir, "-check", "-only", "validate")
	wantRun(t, "nano gen -check -only validate", code, 0, stdout, "", stderr, "")
}

// An unknown //nano: directive is an error at its line, so a misspelled
// marker cannot drop a step silently. The other steps still run.
func TestUnknownDirective(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	write(t, dir, "greeterr/doc.go", "package greeterr\n\n//nano:mapers\n")
	stdout, stderr, code := nano(t, dir, "-check")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr, `greeterr/doc.go:3: unknown directive //nano:mapers
  fix: known directives are //nano:errors and //nano:error
`)
}

// A //go:generate line that runs nano gen bypasses the pipeline's order.
func TestForbiddenGoGenerate(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "cmd/app/config.go", "package app\n",
		"package app\n\n//go:generate go tool nano gen config -dir .\n")
	stdout, stderr, code := nano(t, dir, "-check")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr,
		`cmd/app/config.go:5: //go:generate runs nano gen config; the pipeline runs this step
  fix: delete the line; run go tool nano gen
`)
}

// A validation gap fails the run, but its file is written and the steps
// after it still run: wire picks up the renamed Config field.
func TestValidateGapStillRunsWire(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "api/openapi.yaml", "note: {type: string, maxLength: 3}",
		"note: {type: string, maxLength: 3, not: {const: x}}")
	edit(t, dir, "cmd/app/config.go", "Greeting GreetingConfig `config:\"greeting\"`",
		"Welcome GreetingConfig `config:\"welcome\"`")
	stdout, stderr, code := nano(t, dir)
	wantRun(t, "nano gen", code, 1, stdout, `wrote api/validate.gen.go
wrote cmd/app/config.gen.go
wrote cmd/app/CONFIG.md
wrote cmd/app/.env.example
wrote cmd/app/wire.gen.go
`, stderr, `api/openapi.yaml:26: not enforced: #/components/schemas/Body/properties/note: not is not enforced
  fix: change the schema, or accept the gap with x-nano-unenforced: "<reason>" on the schema at #/components/schemas/Body/properties/note
`)
}

// A failed step skips the steps that need it; the independent ones run.
func TestFailedStepSkipsDependents(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "greeterr/errors.go", "code=greeting_empty ", "")
	edit(t, dir, "api/openapi.yaml", "note: {type: string, maxLength: 3}", "note: {type: string, maxLength: 2}")
	stdout, stderr, code := nano(t, dir, "-check")
	if code != 1 || !strings.Contains(stderr, "stale api/validate.gen.go\n") ||
		!strings.Contains(stderr, "greeterr/errors.go:11: ") ||
		!strings.Contains(stderr, "nano gen config: skipped: needs errors\nnano gen wire: skipped: needs config\n") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// Project values must be literals or constants, so nano can read them
// without running code.
func TestNonLiteralProjectValue(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "nanogen/project.go", "Spec:   spec,", "Spec:   specPath(),")
	edit(t, dir, "nanogen/project.go", "var Project", "func specPath() string { return spec }\n\nvar Project")
	stdout, stderr, code := nano(t, dir, "-check", "-only", "validate")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr,
		`nanogen/project.go:13: project values must be literals or constants; API.Spec is specPath()
  fix: replace specPath() with a string
nano gen validate: skipped: needs nanogen/project.go
`)
}

// A declared path that does not exist names the documents the module has.
func TestMissingSpec(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "nanogen/project.go", `const spec = "api/openapi.yaml"`, `const spec = "api/openapi.yml"`)
	_, stderr, code := nano(t, dir, "-check", "-only", "validate")
	want := "nanogen/project.go:11: API.Spec api/openapi.yml does not exist\n  fix: the module has api/openapi.yaml\n" +
		"nano gen validate: skipped: needs nanogen/project.go\n"
	if code != 1 || stderr != want {
		t.Fatalf("exit %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
}

// Every -json line is one Event with the documented fields.
func TestJSONEvents(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "cmd/app/config.go", `default:"hello"`, `default:"hi"`)
	write(t, dir, "greeterr/doc.go", "package greeterr\n\n//nano:mapers\n")
	stdout, stderr, code := nano(t, dir, "-check", "-json")
	if code != 1 || stderr != "" {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	allowed := []string{"step", "level", "file", "line", "msg", "fix", "diff", "inputs", "outputs"}
	levels := []string{
		LevelWrote, LevelDeleted, LevelStale, LevelMissing, LevelOrphan, LevelSkipped, LevelError, LevelPlan,
	}
	var got []Event
	for line := range strings.Lines(stdout) {
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		for k := range raw {
			if !slices.Contains(allowed, k) {
				t.Errorf("line %q has the unknown key %q", line, k)
			}
		}
		var e Event
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			t.Fatal(err)
		}
		if e.Step == "" || e.Msg == "" || !slices.Contains(levels, e.Level) {
			t.Errorf("event %+v lacks a step, a msg or a known level", e)
		}
		if (e.Level == LevelError || e.Level == LevelStale) && e.Fix == "" {
			t.Errorf("event %+v has no fix", e)
		}
		got = append(got, e)
	}
	want := []Event{
		{Step: "config", Level: LevelStale, File: "cmd/app/config.gen.go"},
		{Step: "config", Level: LevelStale, File: "cmd/app/CONFIG.md"},
		{Step: "config", Level: LevelStale, File: "cmd/app/.env.example"},
		{Step: StepPlan, Level: LevelError, File: "greeterr/doc.go", Line: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("events = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		if g := got[i]; g.Step != w.Step || g.Level != w.Level || g.File != w.File || g.Line != w.Line {
			t.Errorf("event %d = %+v, want %+v", i, g, w)
		}
	}
}

// -list prints the plan; without nanogen/project.go it also prints a
// declaration filled from what it finds.
func TestList(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := nano(t, fixture, "-list")
	wantRun(t, "nano gen -list", code, 0, stdout, `api       api
  in   api/openapi.yaml
  out  api/api.gen.go
validate  api
  in   api/openapi.yaml
  out  api/validate.gen.go
errors    greeterr
  in   greeterr/errors.go
  out  greeterr/errors.gen.go
  out  greeterr/ERRORS.md
config    cmd/app
  in   cmd/app/manifest.go
  out  cmd/app/config.gen.go
  out  cmd/app/CONFIG.md
  out  cmd/app/.env.example
wire      cmd/app
  in   cmd/app/manifest.go
  out  cmd/app/wire.gen.go
`, stderr, "")

	dir := copyFixture(t)
	if err := os.RemoveAll(filepath.Join(dir, "nanogen")); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = nano(t, dir, "-list", "-only", "errors")
	if code != 0 || !strings.Contains(stdout, `var Project = project.Project{
	API: &project.OpenAPI{
		Spec:   "api/openapi.yaml",
		Server: project.Package{Dir: "api"},
	},
}`) {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-only", "config", "-skip", "wire"},
		{"-only", "db"},
		{"-diff"},
		{"config"},
		{"-no-such-flag"},
	} {
		if _, stderr, code := nano(t, fixture, args...); code != 2 || stderr == "" {
			t.Errorf("nano gen %v: exit %d, stderr %q; want 2 and a message", args, code, stderr)
		}
	}
}

func TestStub(t *testing.T) {
	t.Parallel()
	src := []byte(`// Code generated by nano gen config. DO NOT EDIT.

//go:build !nanogen

package app

import (
	"context"
	"fmt"
	str "strings"
	"gopkg.in/yaml.v3"
)

// Build builds.
func Build(ctx context.Context, cfg Config) (*App, error) {
	// a comment in the body
	return &App{name: fmt.Sprint(cfg.Old), s: str.ToUpper("x")}, yaml.Unmarshal(nil, nil)
}

func (a *App) Name() string { return a.name }

var _ = fmt.Sprintf
`)
	got, ok := stub(src)
	if !ok {
		t.Fatal("stub failed")
	}
	want := `// Code generated by nano gen config. DO NOT EDIT.

//go:build !nanogen

package app

import (
	"context"
	"fmt"
	_ "strings"
	_ "gopkg.in/yaml.v3"
)

// Build builds.
func Build(ctx context.Context, cfg Config) (*App, error) { panic("nano gen stub") }

func (a *App) Name() string { panic("nano gen stub") }

var _ = fmt.Sprintf
`
	if string(got) != want {
		t.Fatalf("stub:\n%s\nwant:\n%s", got, want)
	}
	if _, ok = stub([]byte("package x\nfunc {")); ok {
		t.Fatal("stub of a file that does not parse: ok")
	}
}

func TestDiffLines(t *testing.T) {
	t.Parallel()
	got := diffLines([]byte("a\nb\nc\n"), []byte("a\nB\nc\n"), 20)
	want := []string{"@@ -2,1 +2,1 @@", "-b", "+B"}
	if !slices.Equal(got, want) {
		t.Fatalf("diff = %q, want %q", got, want)
	}
	long := diffLines(nil, bytes.Repeat([]byte("x\n"), 50), 5)
	if len(long) != 5 || long[4] != "... 47 more lines" {
		t.Fatalf("diff = %q", long)
	}
}
