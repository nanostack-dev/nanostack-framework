package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestRewriteGoFile(t *testing.T) {
	in := "package jet\n\nimport \"github.com/go-jet/jet/v2/internal/jet\"\n"
	want := "// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit abc1234.\n" +
		"// See db/UPSTREAM.md for the upstream base and the list of changes.\n" +
		"\n" +
		"package jet\n\nimport \"github.com/nanostack-dev/nanostack-framework/db/internal/jet\"\n"
	if got := string(Rewrite("postgres/x.go", []byte(in), "abc1234")); got != want {
		t.Fatalf("Rewrite() =\n%s\nwant\n%s", got, want)
	}
}

func TestRewriteNonGoFileGetsPathOnly(t *testing.T) {
	in := "module github.com/go-jet/jet/v2/tests\n\nreplace github.com/go-jet/jet/v2 => ../\n"
	want := "module github.com/nanostack-dev/nanostack-framework/db/tests\n\nreplace github.com/nanostack-dev/nanostack-framework/db => ../\n"
	if got := string(Rewrite("tests/go.mod", []byte(in), "abc1234")); got != want {
		t.Fatalf("Rewrite() =\n%s\nwant\n%s", got, want)
	}
}

func TestRewriteLeavesOtherGoJetURLs(t *testing.T) {
	in := "see https://github.com/go-jet/jet/wiki and github.com/go-jet/jet-test-data\n"
	if got := string(Rewrite("README.md", []byte(in), "abc1234")); got != in {
		t.Fatalf("Rewrite() changed unrelated URLs: %s", got)
	}
}

func TestRewriteLeavesBinaryFiles(t *testing.T) {
	in := []byte("github.com/go-jet/jet/v2\x00\x01")
	if got := Rewrite("testdata/blob.go", in, "abc1234"); !bytes.Equal(got, in) {
		t.Fatalf("Rewrite() changed a binary file: %q", got)
	}
}

// The header must match the import byte for byte, or every upstream Go file shows as a
// Nanostack change.
func TestHeaderMatchesImportedFiles(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "internal", "jet", "sql_builder.go"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := recordedBase(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if h := Header(shortSHA(base)); !bytes.HasPrefix(content, []byte(h)) {
		t.Fatalf("internal/jet/sql_builder.go does not start with\n%s", h)
	}
}

func TestRecordedBase(t *testing.T) {
	base, err := recordedBase(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(base) {
		t.Fatalf("recordedBase() = %q", base)
	}
}

// Upstream's nested go.mod files require go-jet at a v2 tag. On the fork's module path
// (no /v2) that version does not parse, so with a local replace it becomes v0.0.0.
func TestRewriteGoModRequireWithLocalReplace(t *testing.T) {
	in := "module github.com/go-jet/jet/v2/examples\n\ngo 1.24.0\n\n" +
		"require (\n\tgithub.com/go-jet/jet/v2 v2.15.0\n\tgithub.com/lib/pq v1.12.3\n)\n\n" +
		"require github.com/go-jet/jet/v2 v2.15.0 // indirect\n\n" +
		"replace github.com/go-jet/jet/v2 => ../\n"
	want := "module github.com/nanostack-dev/nanostack-framework/db/examples\n\ngo 1.24.0\n\n" +
		"require (\n\tgithub.com/nanostack-dev/nanostack-framework/db v0.0.0\n\tgithub.com/lib/pq v1.12.3\n)\n\n" +
		"require github.com/nanostack-dev/nanostack-framework/db v0.0.0 // indirect\n\n" +
		"replace github.com/nanostack-dev/nanostack-framework/db => ../\n"
	if got := string(Rewrite("examples/go.mod", []byte(in), "abc1234")); got != want {
		t.Fatalf("Rewrite() =\n%s\nwant\n%s", got, want)
	}
}

// Without a local replace the version is kept: v0.0.0 would not resolve either, and the
// broken file must stay visible in the diff.
func TestRewriteGoModRequireWithoutReplaceKeepsVersion(t *testing.T) {
	in := "module example.com/x\n\nrequire github.com/go-jet/jet/v2 v2.15.0\n"
	want := "module example.com/x\n\nrequire github.com/nanostack-dev/nanostack-framework/db v2.15.0\n"
	if got := string(Rewrite("go.mod", []byte(in), "abc1234")); got != want {
		t.Fatalf("Rewrite() =\n%s\nwant\n%s", got, want)
	}
	// Only go.mod files are touched.
	text := "require github.com/go-jet/jet/v2 v2.15.0\nreplace github.com/go-jet/jet/v2 => ../\n"
	if got := string(
		Rewrite("README.md", []byte(text), "abc1234"),
	); got != "require github.com/nanostack-dev/nanostack-framework/db v2.15.0\nreplace github.com/nanostack-dev/nanostack-framework/db => ../\n" {
		t.Fatalf("Rewrite() changed a version outside go.mod: %s", got)
	}
}

// The real nested go.mod files of the fork parse, so the rewrite and the hand edits agree.
func TestNestedGoModsRequireDBAtV0(t *testing.T) {
	for _, p := range []string{"tests/go.mod", "examples/go.mod"} {
		content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(ForkModule) + ` v0\.0\.0\b`).
			Match(content) {
			t.Errorf("%s does not require %s v0.0.0:\n%s", p, ForkModule, content)
		}
	}
}
