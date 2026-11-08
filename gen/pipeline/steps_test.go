package pipeline

import (
	"os"
	"path/filepath"
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
nano gen api: skipped: needs nanogen/project.go
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

// A declaration without an API makes the outputs of api and validate
// orphans. A module without a declaration keeps them: neither step ran.
func TestOrphanAfterRemovedAPI(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	write(t, dir, "nanogen/project.go", `package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

var Project = project.Project{}
`)
	stdout, stderr, code := nano(t, dir, "-check")
	wantRun(t, "nano gen -check", code, 1, stdout, "", stderr, `orphan api/api.gen.go
orphan api/validate.gen.go
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
