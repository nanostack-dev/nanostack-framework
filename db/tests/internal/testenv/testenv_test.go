package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseSources(t *testing.T) {
	cases := []struct {
		in      string
		want    []Source
		wantErr string
	}{
		{"", AllSources, ""},
		{"  ", AllSources, ""},
		{"all", AllSources, ""},
		{"postgres", []Source{Postgres14, Postgres18}, ""},
		{"SQLite, cockroach", []Source{Cockroach, SQLite}, ""},
		{"mysql,,mariadb,", []Source{MySQL, MariaDB}, ""},
		{"sqlite,sqlite", []Source{SQLite}, ""},
		{"oracle", nil, `unknown dialect "oracle"`},
		{" , ", nil, `names no dialect`},
		{",", nil, `names no dialect`},
	}
	for _, c := range cases {
		got, err := parseSources(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("parseSources(%q) error = %v, want %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("parseSources(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
}

// testDataRepo makes a git repository with the layout checkLayout expects and returns it
// with its HEAD.
func testDataRepo(t *testing.T) (dir, head string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir = t.TempDir()
	for _, sub := range []string{"init/postgres", "init/cockroach", "init/mysql", "init/sqlite", "results"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "f.txt"), []byte(sub), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(
			os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@example.invalid",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "--quiet")
	run("add", "--all")
	run("commit", "--quiet", "-m", "data")
	return dir, run("rev-parse", "HEAD")
}

func TestVerifyCheckout(t *testing.T) {
	dir, head := testDataRepo(t)
	if err := verifyCheckoutAt(dir, head); err != nil {
		t.Fatalf("clean checkout rejected: %v", err)
	}
	if err := verifyCheckoutAt(
		dir,
		strings.Repeat("0", 40),
	); err == nil ||
		!strings.Contains(err.Error(), "want "+strings.Repeat("0", 40)) {
		t.Fatalf("wrong HEAD accepted: %v", err)
	}

	// An untracked file is fine: the suites only read tracked files.
	if err := os.WriteFile(filepath.Join(dir, "results", "new.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCheckoutAt(dir, head); err != nil {
		t.Fatalf("untracked file rejected: %v", err)
	}

	// A tracked file changed in place (a truncated SQLite file, a rewritten golden file)
	// keeps HEAD, and must still be rejected.
	if err := os.WriteFile(filepath.Join(dir, "init/sqlite", "f.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCheckoutAt(
		dir,
		head,
	); err == nil || !strings.Contains(err.Error(), "local changes") ||
		!strings.Contains(err.Error(), "init/sqlite/f.txt") {
		t.Fatalf("modified tracked file accepted: %v", err)
	}
}

func TestVerifyCheckoutRejectsMissingLayout(t *testing.T) {
	dir, head := testDataRepo(t)
	if err := os.RemoveAll(filepath.Join(dir, "results")); err != nil {
		t.Fatal(err)
	}
	// Deleting tracked files also makes the tree dirty; either check may report it.
	if err := verifyCheckoutAt(dir, head); err == nil {
		t.Fatal("checkout without results/ accepted")
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiffModels(t *testing.T) {
	want, got := t.TempDir(), t.TempDir()
	model := GenTestDataDir + "/sqlite/sakila/model/"
	writeTree(
		t,
		want,
		map[string]string{model + "same.go": "same", model + "changed.go": "old\n", model + "gone.go": "x"},
	)
	writeTree(
		t,
		got,
		map[string]string{model + "same.go": "same", model + "changed.go": "new\n", model + "added.go": "y"},
	)

	report, err := DiffModels(SQLite, want, got, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"only in the committed models: .gentestdata/sqlite/sakila/model/gone.go",
		"only in the generator output: .gentestdata/sqlite/sakila/model/added.go",
		"differs: .gentestdata/sqlite/sakila/model/changed.go",
		"-", "old", "+", "new",
	} {
		if !strings.Contains(report, line) {
			t.Errorf("report misses %q:\n%s", line, report)
		}
	}
	if strings.Contains(report, "same.go") {
		t.Errorf("report lists an identical file:\n%s", report)
	}

	if same, sameErr := DiffModels(SQLite, want, want, 3); sameErr != nil || same != "" {
		t.Fatalf("identical trees: report %q, err %v", same, sameErr)
	}
}

func TestReplaceModelsRefusesEmptyOutput(t *testing.T) {
	root, staging := t.TempDir(), t.TempDir()
	committed := map[string]string{}
	for _, dir := range ModelDirs(SQLite) {
		committed[GenTestDataDir+"/"+dir+"/model/a.go"] = "committed"
	}
	writeTree(t, root, committed)

	// The generator wrote everything but one directory (an empty database file).
	partial := map[string]string{}
	for i, dir := range ModelDirs(SQLite) {
		if i == 1 {
			continue
		}
		partial[GenTestDataDir+"/"+dir+"/model/a.go"] = "generated"
	}
	writeTree(t, staging, partial)
	if err := ReplaceModels(SQLite, staging, root); err == nil || !strings.Contains(err.Error(), "wrote nothing") {
		t.Fatalf("ReplaceModels accepted empty output: %v", err)
	}
	for name := range committed {
		if data, err := os.ReadFile(
			filepath.Join(root, filepath.FromSlash(name)),
		); err != nil ||
			string(data) != "committed" {
			t.Fatalf("%s changed after a refused replace: %q, %v", name, data, err)
		}
	}

	// Complete output replaces every directory, stale files included.
	writeTree(t, root, map[string]string{GenTestDataDir + "/" + ModelDirs(SQLite)[0] + "/model/stale.go": "old"})
	full := map[string]string{}
	for _, dir := range ModelDirs(SQLite) {
		full[GenTestDataDir+"/"+dir+"/model/a.go"] = "generated"
	}
	writeTree(t, staging, full)
	if err := ReplaceModels(SQLite, staging, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(
		filepath.Join(root, GenTestDataDir, ModelDirs(SQLite)[0], "model", "stale.go"),
	); !os.IsNotExist(
		err,
	) {
		t.Fatalf("stale file kept: %v", err)
	}
}

func TestChildTestArgs(t *testing.T) {
	in := []string{
		"-test.paniconexit0", "-test.timeout=10m0s", "-test.v=true", "-test.run=TestX",
		"-test.count=1", "-test.shuffle=42", "-test.testlogfile=/tmp/log", "-test.coverprofile", "/tmp/c.out",
		"-test.short=true",
	}
	got, err := childTestArgs(in, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-test.paniconexit0",
		"-test.v=true",
		"-test.run=TestX",
		"-test.count=1",
		"-test.shuffle=42",
		"-test.short=true",
	}
	// The remaining timeout comes from the test.timeout flag of this binary, if any.
	if n := len(got); n > 0 && strings.HasPrefix(got[n-1], "-test.timeout=") {
		got = got[:n-1]
	}
	if !slices.Equal(got, want) {
		t.Fatalf("childTestArgs() = %v, want %v", got, want)
	}
}

func TestRemoveStaleRunDirs(t *testing.T) {
	root := t.TempDir()
	mk := func(name, pid string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if pid != "" {
			if err := os.WriteFile(filepath.Join(dir, pidFile), []byte(pid), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	// A process that has exited: start one and wait for it.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("cannot run a short-lived process:", err)
	}
	dead := mk("postgres-dead", strconv.Itoa(cmd.ProcessState.Pid()))
	alive := mk("postgres-alive", strconv.Itoa(os.Getpid()))
	fresh := mk("mysql-starting", "")
	old := mk("mysql-abandoned", "")
	past := time.Now().Add(-2 * staleAfter)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	removeStaleRunDirs(root, time.Now())
	for dir, keep := range map[string]bool{dead: false, alive: true, fresh: true, old: false} {
		_, err := os.Stat(dir)
		if exists := err == nil; exists != keep {
			t.Errorf("%s: exists = %v, want %v", filepath.Base(dir), exists, keep)
		}
	}
}

// Each run works in its own directory, so the generator suites' relative output
// (./.gentestdata2, ./.tempTestDir) never collides with another go test process, and
// ../testdata still resolves.
func TestRunInUsesTheRunWorkDir(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "root-sqlite", "sqlite")
	if err = os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	var during string
	h := &harness{suite: Suite{Name: "sqlite", Run: func(*Env) int {
		during, _ = os.Getwd()
		return 3
	}}}
	if code := h.runIn(&Env{Source: SQLite, WorkDir: work}); code != 3 {
		t.Fatalf("runIn returned %d, want the suite's code 3", code)
	}
	if want, _ := filepath.EvalSymlinks(work); during != want && during != work {
		t.Fatalf("suite ran in %s, want %s", during, work)
	}
	if after, _ := os.Getwd(); after != before {
		t.Fatalf("working directory not restored: %s, want %s", after, before)
	}
}

func TestSelectedByUser(t *testing.T) {
	cases := []struct {
		run, skip string
		want      bool
	}{
		{"", "", true},
		{"TestCast", "", true},
		{"^TestCast$/sub", "", true},
		{"TestSelect", "", false},
		{"", "TestCast", false},
		{"", "TestOther", true},
	}
	for _, c := range cases {
		if got := selectedByUser("TestCast", c.run, c.skip); got != c.want {
			t.Errorf("selectedByUser(TestCast, %q, %q) = %v, want %v", c.run, c.skip, got, c.want)
		}
	}
}

func TestKnownFailuresAreComplete(t *testing.T) {
	for _, k := range knownFailures {
		if k.Suite == "" || k.Source == "" || k.Test == "" || k.Signature == "" || k.Reason == "" {
			t.Errorf("incomplete known failure: %+v", k)
		}
		if !slices.Contains(AllSources, k.Source) {
			t.Errorf("%s: unknown source %q", k.Test, k.Source)
		}
	}
	upstream, err := os.ReadFile(filepath.Join("..", "..", "..", "UPSTREAM.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range knownFailures {
		if !strings.Contains(string(upstream), "`"+k.Test+"`") {
			t.Errorf("db/UPSTREAM.md does not record the known failure %s", k.Test)
		}
	}
}
