package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/utils/repo"
)

// CommittedSources are the sources whose generated models are committed in
// db/tests/.gentestdata and compiled into the suites.
var CommittedSources = []Source{Postgres18, MySQL, SQLite}

// IsCommitted reports whether s is one of CommittedSources.
func IsCommitted(s Source) bool { return slices.Contains(CommittedSources, s) }

// CheckModels checks the models env's binary was compiled against.
//
// For a committed source it fails t when the committed models differ from the ones the
// harness just generated from env's database. For any other source it checks that the
// binary was compiled against that source's own generator output: either the trees are
// identical, or the harness ran the suite in a child compiled with an overlay of the
// generated models. It logs how they differ from the committed ones.
func CheckModels(t *testing.T, env *Env) {
	t.Helper()
	if env == nil {
		t.Fatal("testenv: no environment; run the suite through testenv.Run")
	}

	maxDiffs := 3
	if !IsCommitted(env.Source) {
		maxDiffs = 0
	}
	report, err := DiffModels(env.Source, repo.GetTestsDirPath(), env.TestRoot, maxDiffs)
	if err != nil {
		t.Fatal(err)
	}

	if !IsCommitted(env.Source) {
		if env.Models != ModelsGenerated {
			t.Fatalf("the suite runs against %s with the committed %s models, not the ones generated from %s:\n%s",
				env.Source, committedFor(env.Source), env.Source, report)
		}
		if report != "" {
			t.Logf("compiled against the models generated from %s, which differ from the committed %s models:\n%s",
				env.Source, committedFor(env.Source), report)
		}
		return
	}

	if report != "" {
		t.Fatalf("the committed models in db/tests/%s do not match the generator output for %s:\n%s\n"+
			"Refresh them with: cd db/tests && go run ./tools/gentestdata", GenTestDataDir, env.Source, report)
	}
}

// committedFor returns the committed source whose models s shares.
func committedFor(s Source) Source {
	switch {
	case s.IsPostgres() || s == Cockroach:
		return Postgres18
	case s == MySQL || s == MariaDB:
		return MySQL
	}
	return SQLite
}

// ReplaceModels moves the model directories of s from staging/.gentestdata into
// root/.gentestdata. It checks every directory first and changes nothing when the
// generator wrote no file for one of them: an empty result means a broken database or
// test data, never that the models should be deleted.
func ReplaceModels(s Source, staging, root string) error {
	trees := map[string]map[string][]byte{}
	for _, dir := range ModelDirs(s) {
		files, err := readTree(filepath.Join(staging, GenTestDataDir, dir))
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf(
				"the generator wrote nothing for %s/%s from %s; the committed models are left unchanged",
				GenTestDataDir,
				dir,
				s,
			)
		}
		trees[dir] = files
	}
	for dir, files := range trees {
		dst := filepath.Join(root, GenTestDataDir, dir)
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		for name, content := range files {
			path := filepath.Join(dst, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return err
			}
			if err := os.WriteFile(path, content, 0o644); err != nil { // #nosec G306 -- source files
				return err
			}
		}
	}
	return nil
}
