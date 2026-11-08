// Package gopkg holds helpers the generators share for reading the package
// they generate into.
package gopkg

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// PackageName reads the package clause of the first non-test Go file in dir
// other than out, the generated output, which may be stale or broken.
func PackageName(dir, out string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", gen.Errorf(dir, 0, "pass -dir with the package directory", "the directory does not exist")
	}
	if err != nil {
		return "", &gen.Error{File: dir, Msg: err.Error(), Fix: "make the directory readable", Err: gen.ErrEnvironment}
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || n == out {
			continue
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.PackageClauseOnly)
		if parseErr != nil {
			return "", gen.Errorf(filepath.Join(dir, n), 0, "fix the package clause", "%v", parseErr)
		}
		return f.Name.Name, nil
	}
	return "", gen.Errorf(dir, 0, "pass -dir with the directory of a Go package", "no Go files besides %s", out)
}

// Overlay returns the overlay of one load: the caller's files, then the
// generator's own, which win for the same path. Every caller path must be
// absolute, as go/packages requires.
func Overlay(caller, own map[string][]byte) (map[string][]byte, error) {
	out := make(map[string][]byte, len(caller)+len(own))
	for path, data := range caller {
		if !filepath.IsAbs(path) {
			return nil, gen.Errorf("", 0, "key Options.Overlay by absolute path, for example with filepath.Abs",
				"overlay path %s is not absolute", path)
		}
		out[filepath.Clean(path)] = data
	}
	maps.Copy(out, own)
	return out, nil
}

// LoadFailed reports a packages.Load that returned an error: the go command
// failed before any package was type-checked. The usual cause is the
// machine (no go command, no toolchain, no network for a module download),
// so it wraps ErrEnvironment. A go.mod that does not parse fails here too;
// the fix names it, and the message carries the go command's report.
func LoadFailed(dir string, err error) *gen.Error {
	return &gen.Error{
		Msg: fmt.Sprintf("load the package in %s: %v", dir, err),
		Fix: "fix what the go command reports: check that go is on PATH, that the directory is inside a " +
			"Go module, and that its go.mod parses",
		Err: gen.ErrEnvironment,
	}
}

// CompileErrors turns the errors go/packages reports for pkg into problems
// at their file and line, with paths shortened by rel and the same fix on
// each, except an error that says a type lacks a method of a tag interface:
// its fix is the method to add. The go command's compile report repeats the type errors without
// positions, so it is dropped when any other error remains.
func CompileErrors(pkg *packages.Package, rel func(string) string, fix string) []error {
	kept := make([]packages.Error, 0, len(pkg.Errors))
	for _, e := range pkg.Errors {
		if !IsCompileReport(pkg, e) {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		kept = pkg.Errors
	}
	out := make([]error, 0, len(kept))
	for _, e := range kept {
		file, line := SplitPos(e.Pos)
		if file != "" {
			file = rel(file)
		}
		f := fix
		if tagFix, ok := tagMethodFix(pkg, e.Msg); ok {
			f = tagFix
		}
		out = append(out, gen.Errorf(file, line, f, "%s", e.Msg))
	}
	return out
}

// Problems turns go/packages errors into problems at their file and line,
// with paths shortened by rel and the same fix on each.
func Problems(errs []packages.Error, rel func(string) string, fix string) []error {
	out := make([]error, 0, len(errs))
	for _, e := range errs {
		file, line := SplitPos(e.Pos)
		if file != "" {
			file = rel(file)
		}
		out = append(out, gen.Errorf(file, line, fix, "%s", e.Msg))
	}
	return out
}

// IsCompileReport reports whether e is the go command's compile output for
// the package itself. go list compiles the package for export data and
// reports its type errors as one list error; the type check of the source
// reports the same errors one by one, with positions.
func IsCompileReport(pkg *packages.Package, e packages.Error) bool {
	return e.Kind == packages.ListError && strings.HasPrefix(e.Msg, "# "+pkg.PkgPath+"\n")
}

// SplitPos splits a go/packages position, "file:line:col" or "file:line",
// into the file and the line. An empty or "-" position gives "" and 0.
func SplitPos(pos string) (string, int) {
	if pos == "" || pos == "-" {
		return "", 0
	}
	parts := strings.Split(pos, ":")
	// The file name may hold colons; the line and column are the last parts.
	for n := min(len(parts)-1, 2); n >= 1; n-- {
		line, err := strconv.Atoi(parts[len(parts)-n])
		if err != nil {
			continue
		}
		if n == 2 {
			if _, colErr := strconv.Atoi(parts[len(parts)-1]); colErr != nil {
				continue
			}
		}
		return strings.Join(parts[:len(parts)-n], ":"), line
	}
	return pos, 0
}
