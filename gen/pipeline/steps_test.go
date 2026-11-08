package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests of which steps run: a broken declaration, a removed API and the
// stubs of later outputs. The helpers are in pipeline_test.go.

// A declaration that cannot be read skips the steps that read it, and the
// steps that need them: a run must not write the Env file to its default
// path and delete the declared one as an orphan.
func TestBrokenProjectSkipsItsSteps(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "nanogen/project.go", "\n}\n", "\n\tEnv: \"app.env\",\n}\n")
	stdout, stderr, code := nano(t, dir)
	wantRun(t, "nano gen with Env", code, 0, stdout, "wrote app.env\ndeleted cmd/app/.env.example\n", stderr, "")

	edit(t, dir, "nanogen/project.go", `Env: "app.env"`, "Env: 1")
	stdout, stderr, code = nano(t, dir)
	wantRun(t, "nano gen with a broken declaration", code, 1, stdout, "", stderr,
		`nanogen/project.go:14: cannot use 1 (untyped int constant) as string value in struct literal
  fix: make nanogen/project.go compile
nano gen validate: skipped: needs nanogen/project.go
nano gen config: skipped: needs nanogen/project.go
nano gen wire: skipped: needs config
`)
	if _, err := os.Stat(filepath.Join(dir, "app.env")); err != nil {
		t.Fatalf("the declared Env file is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "app", ".env.example")); !os.IsNotExist(err) {
		t.Fatalf("cmd/app/.env.example was written: %v", err)
	}
}

// A declaration without an API makes validate's output an orphan. A module
// without a declaration keeps it: validate did not run.
func TestOrphanAfterRemovedAPI(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	write(t, dir, "nanogen/project.go", `package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

var Project = project.Project{}
`)
	stdout, stderr, code := nano(t, dir, "-check")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr, `orphan api/validate.gen.go
  fix: run nano gen without -check, then commit the result
`)

	if err := os.RemoveAll(filepath.Join(dir, "nanogen")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = nano(t, dir, "-check")
	wantRun(t, "nano gen -check without a declaration", code, 0, stdout, "", stderr, "")
}

// An existing output of a later step is a stub in every load, so a stale
// body in it cannot break an earlier step. Here a catalogue sits in the
// manifest package: errors loads cmd/app without the nanogen tag, with the
// config.gen.go and wire.gen.go that still read the renamed Config field.
func TestLaterOutputIsStubbed(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	write(t, dir, "cmd/app/errors.go", `package app

// declarations is the app error catalogue.
//
//nano:errors namespace=app
type declarations interface {
	// The app is missing.
	//
	//nano:error code=app_missing status=404 title="App is missing"
	AppMissing()
}
`)
	edit(t, dir, "cmd/app/config.go", "Greeting GreetingConfig `config:\"greeting\"`",
		"Welcome GreetingConfig `config:\"welcome\"`")

	stdout, stderr, code := nano(t, dir, "-check")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr, `missing cmd/app/errors.gen.go
missing cmd/app/ERRORS.md
stale cmd/app/config.gen.go
stale cmd/app/CONFIG.md
stale cmd/app/.env.example
stale cmd/app/wire.gen.go
  fix: run nano gen without -check, then commit the result
`)
	stdout, stderr, code = nano(t, dir)
	wantRun(t, "nano gen", code, 0, stdout, `wrote cmd/app/errors.gen.go
wrote cmd/app/ERRORS.md
wrote cmd/app/config.gen.go
wrote cmd/app/CONFIG.md
wrote cmd/app/.env.example
wrote cmd/app/wire.gen.go
`, stderr, "")
}

// A file the run cannot read is the machine's problem, not the source's:
// exit 3, with the fix.
func TestUnreadableOutputExitsThree(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	path := filepath.Join(dir, "cmd", "app", "CONFIG.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := nano(t, dir, "-check", "-only", "config")
	if code != 3 || !strings.Contains(stderr, "cmd/app/CONFIG.md: ") ||
		!strings.Contains(stderr, "  fix: make the path a writable regular file, or remove what is in its place\n") {
		t.Fatalf("exit %d, want 3; stderr:\n%s", code, stderr)
	}
}

// A Go file that does not parse may hold a declaration, so the orphan scan
// does not run: the outputs of a catalogue whose file is broken stay.
func TestParseErrorKeepsOutputs(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	write(t, dir, "greeterr/errors.go", "package greeterr\n\nfunc {\n")
	stdout, stderr, code := nano(t, dir, "-only", "errors")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "greeterr/errors.go:3: ") ||
		strings.Contains(stderr, "orphan") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "greeterr", "errors.gen.go")); err != nil {
		t.Fatalf("errors.gen.go was deleted: %v", err)
	}
}

// A misspelled //nano:errors drops its catalogue from the plan. The run
// reports the directive and keeps the catalogue's outputs, instead of
// deleting them as orphans.
func TestMisspelledDirectiveKeepsOutputs(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "greeterr/errors.go", "//nano:errors", "//nano:erors")
	stdout, stderr, code := nano(t, dir, "-only", "errors")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "unknown directive //nano:erors") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, f := range []string{"errors.gen.go", "ERRORS.md"} {
		if _, err := os.Stat(filepath.Join(dir, "greeterr", f)); err != nil {
			t.Fatalf("%s was deleted: %v", f, err)
		}
	}
}

// -skip runs every other step: config and wire are stale, but skipped, so
// -check passes and their files are not orphans.
func TestSkip(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "cmd/app/config.go", "Greeting GreetingConfig `config:\"greeting\"`",
		"Welcome GreetingConfig `config:\"welcome\"`")
	stdout, stderr, code := nano(t, dir, "-check", "-skip", "config,wire")
	wantRun(t, "nano gen -check -skip config,wire", code, 0, stdout, "", stderr, "")
	_, _, code = nano(t, dir, "-check", "-skip", "wire")
	if code != 1 {
		t.Fatalf("nano gen -check -skip wire: exit %d, want 1 for the stale config", code)
	}
}

// A declared Env with two Config packages would make both write one file.
func TestEnvSharedByTwoConfigs(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "nanogen/project.go", "\n}\n", "\n\tEnv: \"app.env\",\n}\n")
	write(t, dir, "cmd/other/manifest.go", `//go:build nanogen

package other

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("other")
`)
	_, stderr, code := nano(t, dir, "-list")
	want := "nanogen/project.go: app.env is written by config cmd/app and by config cmd/other\n" +
		"  fix: remove Env from nanogen/project.go, so each Config package writes the .env.example next to it\n"
	if code != 1 || stderr != want {
		t.Fatalf("exit %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
}

// validate writes into API.Server.Dir, so it must stay inside the directory
// the run covers.
func TestServerDirOutside(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "nanogen/project.go", `Server: project.Package{Dir: "api"}`, `Server: project.Package{Dir: "../api"}`)
	_, stderr, code := nano(t, dir, "-check", "-only", "validate")
	want := "nanogen/project.go:12: API.Server.Dir ../api is outside the directory nano gen runs on\n" +
		"  fix: keep the server package inside the directory nano gen runs on, or run nano gen on a directory " +
		"that holds it\nnano gen validate: skipped: needs nanogen/project.go\n"
	if code != 1 || stderr != want {
		t.Fatalf("exit %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
}

// nano reads only the literal of var Project: a value set anywhere else,
// such as in init, would be ignored, so it is an error.
func TestProjectSetOutsideItsLiteral(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	edit(t, dir, "nanogen/project.go", "\n}\n", "\n}\n\nfunc init() { Project.Env = \"app.env\" }\n")
	_, stderr, code := nano(t, dir, "-check", "-only", "validate")
	want := "nanogen/project.go:16: nano gen reads only the literal of var Project; this use of Project is ignored\n" +
		"  fix: set every value in the var Project literal, and do not refer to Project elsewhere\n" +
		"nano gen validate: skipped: needs nanogen/project.go\n"
	if code != 1 || stderr != want {
		t.Fatalf("exit %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
}
