package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a tiny go-jet-like upstream repository with two commits, and a fork of the
// first commit that has its own changes.
type fixture struct {
	upstream string // git repository
	fork     string // the db module root
	c1, c2   string // upstream commits
}

func lines(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(prefix)
		b.WriteString(" line ")
		b.WriteByte(byte('0' + i))
		b.WriteString(" " + strings.Repeat(prefix+"-", 60) + "\n") // distinct files, no rename detection
	}
	return b.String()
}

func goFile(name, body string) string {
	return "package fix\n\nimport \"github.com/go-jet/jet/v2/internal/" + name + "\"\n\n" + body
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	f := fixture{upstream: t.TempDir(), fork: t.TempDir()}
	gitT := func(args ...string) string {
		t.Helper()
		out, err := git(t.Context(), f.upstream, []string{
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		}, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(files map[string]string) {
		t.Helper()
		for p, content := range files {
			if err := writeFile(filepath.Join(f.upstream, p), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	gitT("init", "--quiet")
	write(map[string]string{
		"go.mod":                  "module github.com/go-jet/jet/v2\n\ngo 1.24.0\n",
		"README.md":               "go-jet\n",
		"a.go":                    goFile("a", lines("a", 9)),
		"b.go":                    goFile("b", lines("b", 9)),
		"c.go":                    goFile("c", lines("c", 3)),
		"d.go":                    goFile("d", lines("d", 3)),
		"e.go":                    goFile("e", lines("e", 3)),
		"f.go":                    goFile("f", lines("f", 3)),
		"g.go":                    goFile("g", lines("g", 3)),
		"h.go":                    goFile("h", lines("h", 3)),
		"tests/.gentestdata/x.go": goFile("x", lines("x", 3)),
		".circleci/config.yml":    "version: 1\n",
		".gitignore":              ".gen\n",
		"examples/.gen/model.go":  goFile("model", lines("m", 2)),
	})
	gitT("add", "--all")
	gitT("add", "--force", "examples/.gen/model.go") // go-jet force-adds generated example models
	gitT("update-index", "--add", "--cacheinfo", "160000,"+testDataPin1+","+testDataPath)
	gitT("commit", "--quiet", "-m", "c1")
	f.c1 = gitT("rev-parse", "HEAD")

	write(map[string]string{
		"a.go":                    goFile("a", strings.Replace(lines("a", 9), "a line 2", "a line 2 upstream", 1)),
		"b.go":                    goFile("b", strings.Replace(lines("b", 9), "b line 5", "b line 5 upstream", 1)),
		"e.go":                    goFile("e", lines("e", 4)),
		"h.go":                    goFile("h", lines("h", 4)),
		"new.go":                  goFile("new", lines("new", 2)),
		"collide.go":              goFile("collide", lines("upstream", 2)),
		"tests/.gentestdata/x.go": goFile("x", lines("x", 5)),
		".circleci/config.yml":    "version: 2\n",
		"README.md":               "go-jet\nupstream README edit\n",
	})
	gitT("rm", "--quiet", "c.go", "d.go")
	gitT("add", "--all")
	gitT("update-index", "--add", "--cacheinfo", "160000,"+testDataPin2+","+testDataPath)
	gitT("commit", "--quiet", "-m", "c2")
	gitT("tag", "v2")
	f.c2 = gitT("rev-parse", "HEAD")

	// The fork: the import of c1, then Nanostack changes.
	if _, err := extract(t.Context(), f.upstream, f.c1, target{f.fork, shortSHA(f.c1)}); err != nil {
		t.Fatal(err)
	}
	f.edit(t, "a.go", "a line 9", "a line 9 nanostack")
	f.edit(t, "b.go", "b line 5", "b line 5 nanostack")
	f.edit(t, "d.go", "d line 1", "d line 1 nanostack")
	f.edit(t, "go.mod", "go 1.24.0", "go 1.27")
	for _, p := range []string{"e.go", "f.go"} {
		if err := os.Remove(filepath.Join(f.fork, p)); err != nil {
			t.Fatal(err)
		}
	}
	f.put(t, "nanostack.go", "package fix\n")
	f.put(t, "collide.go", "package fix\n// Nanostack\n")
	f.put(t, "tests/.gentestdata/x.go", "package regenerated\n")
	f.put(
		t,
		"UPSTREAM.md",
		"| Base commit | `"+f.c1+"` (2026-11-07) |\n| Test data | jet-test-data at `"+testDataPin1+"` |\n",
	)
	f.put(t, testDataCommitFile, "package testenv\n\nconst TestDataCommit = \""+testDataPin1+"\"\n")
	// go-jet's README moves to docs/upstream/ (with a Nanostack banner); README.md is ours.
	f.put(t, "docs/upstream/README.md", "> banner\n\n"+f.read(t, "README.md"))
	f.put(t, "README.md", "Nanostack DB\n")
	if err := os.RemoveAll(f.path(".circleci")); err != nil {
		t.Fatal(err)
	}
	return f
}

const (
	testDataPin1 = "1111111111111111111111111111111111111111"
	testDataPin2 = "2222222222222222222222222222222222222222"
)

func (f fixture) path(p string) string { return filepath.Join(f.fork, filepath.FromSlash(p)) }

func (f fixture) read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(f.path(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f fixture) put(t *testing.T, p, content string) {
	t.Helper()
	if err := writeFile(f.path(p), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) edit(t *testing.T, p, old, replacement string) {
	t.Helper()
	content := f.read(t, p)
	if !strings.Contains(content, old) {
		t.Fatalf("%s does not contain %q", p, old)
	}
	f.put(t, p, strings.Replace(content, old, replacement, 1))
}

func (f fixture) exists(p string) bool {
	_, err := os.Stat(f.path(p))
	return err == nil
}

func (f fixture) run(t *testing.T, o options) (string, int) {
	t.Helper()
	o.dbDir, o.repo = f.fork, f.upstream
	var out bytes.Buffer
	conflicts, err := run(t.Context(), o, &out)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	return out.String(), conflicts
}

// section returns the text of one numbered output section.
func section(out string, n string) string {
	start := strings.Index(out, "== "+n+".")
	if start < 0 {
		return ""
	}
	rest := out[start+3:]
	if before, _, ok := strings.Cut(rest, "\n== "); ok {
		return before
	}
	return rest
}

func TestDiffSameRefReportsOnlyNanostackChanges(t *testing.T) {
	f := newFixture(t)
	out, conflicts := f.run(t, options{ref: f.c1})
	if conflicts != 0 {
		t.Fatalf("conflicts = %d", conflicts)
	}
	if got := strings.TrimSpace(strings.SplitN(section(out, "1"), "\n", 2)[1]); got != "(none)" {
		t.Fatalf("upstream section = %q, want (none)", got)
	}
	ours := section(out, "2")
	for _, p := range []string{"a.go", "b.go", "d.go", "e.go", "f.go", "go.mod"} {
		if !strings.Contains(ours, "diff --git a/"+p+" ") {
			t.Errorf("Nanostack section misses %s:\n%s", p, ours)
		}
	}
	if !strings.Contains(ours, "diff --git a/README.md b/README.md") || !strings.Contains(ours, "+> banner") {
		t.Errorf("Nanostack section does not compare upstream README.md with docs/upstream/README.md:\n%s", ours)
	}
	if strings.Contains(ours, "Nanostack DB") {
		t.Errorf("Nanostack section compares upstream README.md with db/README.md:\n%s", ours)
	}
	for _, p := range []string{"g.go", "nanostack.go", "collide.go", "UPSTREAM.md", "tests/.gentestdata", "docs/"} {
		if strings.Contains(ours, "diff --git a/"+p) {
			t.Errorf("Nanostack section lists %s:\n%s", p, ours)
		}
	}
}

func TestDiffShowsRewrittenUpstreamChanges(t *testing.T) {
	f := newFixture(t)
	out, _ := f.run(t, options{ref: "v2"})
	up := section(out, "1")
	for _, want := range []string{
		"diff --git a/a.go b/a.go", "+a line 2 upstream",
		"diff --git a/new.go b/new.go",
		"+// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit " + shortSHA(f.c1) + ".",
		"+import \"github.com/nanostack-dev/nanostack-framework/db/internal/new\"",
		"deleted file mode", "diff --git a/c.go b/c.go",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("upstream section misses %q:\n%s", want, up)
		}
	}
	for _, unwanted := range []string{"github.com/go-jet/jet/v2", "g.go", ".gentestdata", "commit " + shortSHA(f.c2)} {
		if strings.Contains(up, unwanted) {
			t.Errorf("upstream section contains %q:\n%s", unwanted, up)
		}
	}
	if !strings.Contains(out, "Added upstream but already present in Nanostack DB: collide.go") {
		t.Errorf("collision not reported:\n%s", out)
	}
}

func TestApplyMergesAndListsConflicts(t *testing.T) {
	f := newFixture(t)
	gentest := f.read(t, "tests/.gentestdata/x.go")
	out, conflicts := f.run(t, options{ref: "v2", apply: true})
	t.Log(out)

	header := Header(shortSHA(f.c2))
	importLine := "import \"github.com/nanostack-dev/nanostack-framework/db/internal/"

	// Clean 3-way merge: both edits kept, header moved to the new upstream commit.
	a := f.read(t, "a.go")
	if !strings.HasPrefix(a, header) || !strings.Contains(a, "a line 2 upstream") ||
		!strings.Contains(a, "a line 9 nanostack") {
		t.Errorf("a.go not merged:\n%s", a)
	}
	// Unchanged upstream content: only the header moves.
	if g := f.read(t, "g.go"); g != string(Rewrite("g.go", []byte(goFile("g", lines("g", 3))), shortSHA(f.c2))) {
		t.Errorf("g.go header not updated:\n%s", g)
	}
	if h := f.read(t, "h.go"); h != string(Rewrite("h.go", []byte(goFile("h", lines("h", 4))), shortSHA(f.c2))) {
		t.Errorf("h.go not updated to upstream:\n%s", h)
	}
	if n := f.read(t, "new.go"); !strings.HasPrefix(n, header) || !strings.Contains(n, importLine+"new\"") {
		t.Errorf("new.go not added rewritten:\n%s", n)
	}
	if f.exists("c.go") {
		t.Error("c.go deleted upstream and unchanged here, but still present")
	}
	if !f.exists("d.go") || !strings.Contains(f.read(t, "d.go"), "d line 1 nanostack") {
		t.Error("d.go changed here and deleted upstream, but not kept")
	}
	if f.exists("e.go") || f.exists("f.go") {
		t.Error("a file deleted in Nanostack DB was restored")
	}
	if b := f.read(
		t,
		"b.go",
	); !strings.Contains(b, "<<<<<<< Nanostack DB") ||
		!strings.Contains(b, ">>>>>>> upstream ref") {
		t.Errorf("b.go has no conflict markers:\n%s", b)
	}
	if got := f.read(t, "tests/.gentestdata/x.go"); got != gentest {
		t.Errorf("excluded tests/.gentestdata/x.go changed:\n%s", got)
	}
	if got := f.read(t, "nanostack.go"); got != "package fix\n" {
		t.Errorf("Nanostack-only file changed:\n%s", got)
	}
	if got := f.read(t, "go.mod"); !strings.Contains(got, "go 1.27") {
		t.Errorf("go.mod lost the Nanostack change:\n%s", got)
	}

	if conflicts != 4 {
		t.Errorf("conflicts = %d, want 4 (b.go, collide.go, d.go, e.go)", conflicts)
	}
	// The relocated README merges into docs/upstream/README.md; db/README.md is untouched.
	if got := f.read(t, "docs/upstream/README.md"); got != "> banner\n\ngo-jet\nupstream README edit\n" {
		t.Errorf("docs/upstream/README.md not merged:\n%s", got)
	}
	if got := f.read(t, "README.md"); got != "Nanostack DB\n" {
		t.Errorf("README.md (Nanostack) changed:\n%s", got)
	}
	if f.exists(".circleci/config.yml") {
		t.Error("dropped .circleci/config.yml was restored")
	}
	// The test data pin moved upstream: the harness constant follows.
	if got, err := harnessTestDataCommit(f.fork); err != nil || got != testDataPin2 {
		t.Errorf("TestDataCommit = %q, %v; want %s", got, err, testDataPin2)
	}
	if !strings.Contains(
		out,
		"test data pin  "+testDataPin1+" -> "+testDataPin2+"  (TestDataCommit in "+testDataCommitFile,
	) {
		t.Errorf("pin move not reported with the harness constant:\n%s", out)
	}
	for _, want := range []string{
		"CONFLICT  b.go:", "CONFLICT  collide.go: added upstream and in Nanostack DB",
		"CONFLICT  d.go: deleted upstream", "CONFLICT  e.go: changed upstream, deleted in Nanostack DB",
		"updated   h.go", "merged    a.go", "merged    README.md", "added     new.go", "deleted   c.go",
		"dropped   .circleci/config.yml", "note      TestDataCommit in " + testDataCommitFile + " set to " + testDataPin2,
		"1 updated, 2 merged, 1 added, 1 deleted, 1 dropped, 2 header-only, 4 conflicts",
	} {
		if !strings.Contains(section(out, "3"), want) {
			t.Errorf("apply report misses %q", want)
		}
	}
	for _, p := range []string{"f.go", "g.go"} {
		if strings.Contains(section(out, "3"), p) {
			t.Errorf("%s (header-only upstream change) listed by name", p)
		}
	}
}

// After apply and a Base update, a rerun against the same ref reports no upstream changes
// and no header noise.
func TestRerunAfterApplyIsClean(t *testing.T) {
	f := newFixture(t)
	f.run(t, options{ref: "v2", apply: true})
	f.put(t, "UPSTREAM.md", "| Base commit | `"+f.c2+"` (2026-11-07) |\n")
	out, _ := f.run(t, options{ref: "v2"})
	if got := strings.TrimSpace(strings.SplitN(section(out, "1"), "\n", 2)[1]); got != "(none)" {
		t.Fatalf("upstream section = %q, want (none)", got)
	}
	if ours := section(out, "2"); strings.Contains(ours, "g.go") || strings.Contains(ours, "new.go") {
		t.Fatalf("Nanostack section shows header-only changes:\n%s", ours)
	}
}

// A file go-jet tracks although its own .gitignore matches it (force-added) must not be
// git-ignored in the fork, or a commit silently drops it.
func TestReportsUpstreamFilesIgnoredInFork(t *testing.T) {
	f := newFixture(t)
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	if _, err := git(t.Context(), f.fork, env, "init", "--quiet"); err != nil {
		t.Fatal(err)
	}
	out, problems := f.run(t, options{ref: f.c1, stat: true})
	if !strings.Contains(out, "IGNORED        examples/.gen/model.go") || problems != 1 {
		t.Fatalf("ignored upstream file not reported (problems = %d):\n%s", problems, out)
	}

	f.put(t, "examples/.gitignore", "!/.gen/\n")
	out, problems = f.run(t, options{ref: f.c1, stat: true})
	if strings.Contains(out, "IGNORED") || problems != 0 {
		t.Fatalf("re-included file still reported (problems = %d):\n%s", problems, out)
	}
}
