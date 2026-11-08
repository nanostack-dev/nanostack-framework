// Package gopkg holds helpers the generators share for reading the package
// they generate into.
package gopkg

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// PackageName reads the package clause of the first non-test Go file in dir
// other than out, the generated output, which may be stale or broken.
func PackageName(dir, out string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || n == out {
			continue
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.PackageClauseOnly)
		if parseErr != nil {
			return "", parseErr
		}
		return f.Name.Name, nil
	}
	return "", fmt.Errorf("%s: no Go files", dir)
}
