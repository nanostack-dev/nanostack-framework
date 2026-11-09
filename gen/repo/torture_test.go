package genrepo

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture"
)

const tortureFixture = "internal/torture"

// tortureOptions are the options of a package that declares tables of the
// torture fixture: three hard tenant columns, one context column, and their
// short names.
func tortureOptions(dir string) Options {
	return Options{
		Dir:             dir,
		Scopes:          torture.Scopes,
		ContextScopes:   torture.ContextScopes,
		ScopeNames:      torture.ScopeNames,
		ScopeImplies:    torture.ScopeImplies,
		ContextStamps:   torture.ContextStamps,
		ContextPresents: torture.ContextPresents,
		DatabaseColumns: torture.DatabaseColumns,
	}
}

// The committed base and derived files of the torture fixture, whose
// integration tests run on PostgreSQL, are what the generator writes, and it
// gets no warning: every method of the fixture derives.
func TestTortureBaseIsCurrent(t *testing.T) {
	t.Parallel()
	res, err := Generate(tortureOptions(tortureFixture))
	if err != nil {
		gentest.Problems(t, err)
		t.Fatal(err)
	}
	// The one tolerated warning: a derived delete of a table with a context scope
	// that has no Present function (region_id) fails open.
	for _, w := range res.Warnings {
		if !strings.Contains(w.Error(), "Zones.DeleteBy") || !strings.Contains(w.Error(), "region_id") {
			t.Errorf("warning on the torture fixture: %v", w)
		}
	}
	for _, f := range res.Files {
		golden(t, f.Path, f.Data)
	}
	// Every method of every interface of the fixture derives: none is hand-written.
	for _, ex := range res.Explain {
		for _, m := range ex.Methods {
			if len(m.How) < 7 || m.How[:7] != "derived" {
				t.Errorf("%s.%s is not derived: %s", ex.Iface, m.Name, m.How)
			}
		}
	}
}

// The names that must not derive on the torture tables are refused, each at
// its line, with the fix in testdata/torture/refused/want.txt. The package's
// base is the torture base under another package clause.
func TestTortureRefusals(t *testing.T) {
	t.Parallel()
	dir := "testdata/torture/refused"
	base, err := os.ReadFile(filepath.Join(tortureFixture, FileName))
	if err != nil {
		t.Fatal(err)
	}
	golden(
		t,
		filepath.Join(dir, FileName),
		bytes.Replace(base, []byte("package torture\n"), []byte("package refused\n"), 1),
	)
	res, err := Generate(tortureOptions(dir))
	if err == nil {
		t.Fatal("no refusal")
	}
	gentest.Problems(t, err)
	if gentest.Environment(err) {
		t.Fatalf("an environment error: %v", err)
	}
	golden(t, filepath.Join(dir, "want.txt"), report(nil, err))
	explainGolden(t, filepath.Join(dir, "want-explain.txt"), res)
	// Every name of the package is refused: none derives, and none is hand-written.
	methods := 0
	for _, ex := range res.Explain {
		for _, m := range ex.Methods {
			methods++
			if !strings.HasPrefix(m.How, "ERROR") {
				t.Errorf("%s.%s must be refused, and resolves as %q", ex.Iface, m.Name, m.How)
			}
		}
	}
	if methods < 100 {
		t.Errorf("%d refused methods checked, want at least 100", methods)
	}
}
