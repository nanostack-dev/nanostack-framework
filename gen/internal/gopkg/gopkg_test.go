package gopkg_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// TestPackageNameReadsTheClause checks that the name comes from the package
// clause, not from text that looks like one in a comment, and that the
// generated output and test files are skipped.
func TestPackageNameReadsTheClause(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"a_test.go": "package api_test\n",
		"api.go":    "/*\nExample:\npackage wrong\n*/\npackage api\n",
		"a.gen.go":  "package stale\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := gopkg.PackageName(dir, "a.gen.go")
	if err != nil || got != "api" {
		t.Fatalf("PackageName = %q, %v; want api", got, err)
	}
	if _, err = gopkg.PackageName(t.TempDir(), "validate.gen.go"); err == nil {
		t.Fatal("PackageName of an empty directory must fail")
	}
}
