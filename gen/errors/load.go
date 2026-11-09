package generrors

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

const loadMode = packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
	packages.NeedFiles

// loaded is the declaration package, type-checked without the generated file.
type loaded struct {
	dir     string // absolute package directory
	outPath string // absolute path of the generated file
	overlay map[string][]byte
	pkg     *packages.Package
}

// pos is a source position for generator errors, relative to the package.
type pos struct {
	file string
	line int
}

func (p pos) String() string { return fmt.Sprintf("%s:%d", p.file, p.line) }

// errorf returns a problem at p.
func (p pos) errorf(fix, format string, args ...any) *gen.Error {
	return gen.Errorf(p.file, p.line, fix, format, args...)
}

// wrap places a problem without a position at p, after prefix.
func (p pos) wrap(prefix string, e *gen.Error) *gen.Error {
	return gen.Errorf(p.file, p.line, e.Fix, "%s: %s", prefix, e.Msg)
}

// problem returns a problem without a position; the caller places it.
func problem(fix, format string, args ...any) *gen.Error {
	return gen.Errorf("", 0, fix, format, args...)
}

// loadPackage type-checks the package with the generated file replaced by an
// empty one, so a stale or broken previous output never blocks regeneration.
//
// Hand-written files may call the generated API, so type errors outside the
// declaration file are expected here and ignored; verify reports any that
// remain once the new output is in place. Errors that stop type checking
// (go list, syntax) are returned.
func loadPackage(opts Options) (*loaded, error) {
	absDir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, gen.EnvErrorf("run from a directory that exists", "resolve %s: %v", opts.Dir, err)
	}
	pkgName, err := gopkg.PackageName(absDir, opts.Out)
	if err != nil {
		return nil, err
	}
	l := &loaded{dir: absDir, outPath: filepath.Join(absDir, opts.Out), overlay: opts.Overlay}
	l.pkg, err = l.load([]byte("package " + pkgName + "\n"))
	if err != nil {
		return nil, err
	}
	var fatal []packages.Error
	for _, e := range l.pkg.Errors {
		if e.Kind != packages.TypeError && !gopkg.IsCompileReport(l.pkg, e) {
			fatal = append(fatal, e)
		}
	}
	if len(fatal) > 0 {
		return nil, errors.Join(gopkg.Problems(fatal, l.relPath,
			"make the package compile; nanostack gen errors replaces "+opts.Out+" with an empty file")...)
	}
	return l, nil
}

// load type-checks the package with generated as the output file, on top of
// the caller's overlay.
func (l *loaded) load(generated []byte) (*packages.Package, error) {
	overlay, err := gopkg.Overlay(l.overlay, map[string][]byte{l.outPath: generated})
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode:    loadMode,
		Dir:     l.dir,
		Fset:    token.NewFileSet(),
		Overlay: overlay,
	}
	pkgs, err := gopkg.Load(cfg, ".")
	if err != nil {
		return nil, gopkg.LoadFailed(l.dir, err)
	}
	if len(pkgs) != 1 {
		return nil, gen.Errorf("", 0, "pass -dir with the directory of one package",
			"%s holds %d packages", l.dir, len(pkgs))
	}
	return pkgs[0], nil
}

// errorsWithin returns the type errors located in node, the declaration
// interface. Errors elsewhere may come from code that calls the generated
// API; verify checks them once the new output is in place.
func (l *loaded) errorsWithin(node ast.Node) []error {
	start, end := l.pkg.Fset.Position(node.Pos()), l.pkg.Fset.Position(node.End())
	var within []packages.Error
	for _, e := range l.pkg.Errors {
		if file, line := gopkg.SplitPos(e.Pos); file == start.Filename && line >= start.Line && line <= end.Line {
			within = append(within, e)
		}
	}
	return gopkg.Problems(within, l.relPath, "fix the declaration so that it compiles")
}

// syntax returns the hand-written files of the package with their names
// relative to the package directory: neither the generated output nor any
// other generated file.
func (l *loaded) syntax() ([]*ast.File, []string) {
	var files []*ast.File
	var names []string
	for _, f := range l.pkg.Syntax {
		path := l.pkg.Fset.Position(f.Package).Filename
		if path == l.outPath || ast.IsGenerated(f) {
			continue
		}
		files = append(files, f)
		names = append(names, filepath.Base(path))
	}
	return files, names
}

// scopeNames returns every package-level name the hand-written files declare.
func (l *loaded) scopeNames() []string {
	return l.pkg.Types.Scope().Names()
}

// lookup returns the package-level object called name and its position.
func (l *loaded) lookup(name string) (types.Object, pos) {
	obj := l.pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return nil, pos{}
	}
	return obj, l.position(obj.Pos())
}

func (l *loaded) position(p token.Pos) pos {
	at := l.pkg.Fset.Position(p)
	return pos{file: l.relPath(at.Filename), line: at.Line}
}

func (l *loaded) relPath(name string) string {
	if rel, err := filepath.Rel(l.dir, name); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return name
}

// verify type-checks the package with src as the generated file. It is the
// safety net behind the checks in parse.go: whatever they miss, a generated
// file that does not compile fails here, at generation time, with the
// position of the declaration that produced the broken code.
func (l *loaded) verify(c *catalogue, src []byte) error {
	pkg, err := l.load(src)
	if err != nil {
		return err
	}
	if len(pkg.Errors) == 0 {
		return nil
	}
	starts := declLines(c, src)
	out := filepath.Base(l.outPath)
	var genErrs, other []packages.Error
	for _, e := range pkg.Errors {
		if gopkg.IsCompileReport(pkg, e) {
			continue
		}
		if file, _ := gopkg.SplitPos(e.Pos); file != l.outPath {
			other = append(other, e)
			continue
		}
		genErrs = append(genErrs, e)
	}
	if len(genErrs) > 0 {
		problems := make([]error, 0, len(genErrs))
		for _, e := range genErrs {
			_, line := gopkg.SplitPos(e.Pos)
			at, name := c.at, c.iface
			for _, s := range starts {
				if s.line <= line {
					at, name = s.decl.at, s.decl.name
				}
			}
			problems = append(problems, at.errorf(
				"rename the declaration to work around it, and report this gap in the nanostack gen errors checks",
				"%s: generated code does not compile: %s (%s:%d)", name, e.Msg, out, line))
		}
		return errors.Join(problems...)
	}
	if len(other) == 0 {
		other = pkg.Errors
	}
	return errors.Join(gopkg.Problems(other, l.relPath, "update the code that uses the generated "+out)...)
}

type declStart struct {
	line int
	decl *decl
}

// declLines finds where each declaration's code starts in the formatted file:
// at its "var Err<Name> =" line. The list is in file order.
func declLines(c *catalogue, src []byte) []declStart {
	var out []declStart
	for i, line := range strings.Split(string(src), "\n") {
		rest, ok := strings.CutPrefix(line, "var Err")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, " ")
		if j := slices.IndexFunc(c.errors, func(d decl) bool { return d.name == name }); j >= 0 {
			out = append(out, declStart{line: i + 1, decl: &c.errors[j]})
		}
	}
	return out
}
