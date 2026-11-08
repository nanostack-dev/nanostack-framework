package generrors

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

const loadMode = packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
	packages.NeedFiles

// loaded is the declaration package, type-checked without the generated file.
type loaded struct {
	dir     string // absolute package directory
	outPath string // absolute path of the generated file
	pkg     *packages.Package
}

// loadPackage type-checks the package with the generated file replaced by an
// empty one, so a stale or broken previous output never blocks regeneration.
//
// Hand-written files may call the generated API, so type errors outside the
// declaration file are expected here and ignored; verify reports any that
// remain once the new output is in place. Errors that stop type checking
// (go list, syntax) are returned.
func loadPackage(dir, out string) (*loaded, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	pkgName, err := gopkg.PackageName(absDir, out)
	if err != nil {
		return nil, err
	}
	l := &loaded{dir: absDir, outPath: filepath.Join(absDir, out)}
	l.pkg, err = l.load([]byte("package " + pkgName + "\n"))
	if err != nil {
		return nil, err
	}
	var fatal []string
	for _, e := range l.pkg.Errors {
		if e.Kind != packages.TypeError && !isCompileReport(l.pkg, e) {
			fatal = append(fatal, l.rel(e.Error()))
		}
	}
	if len(fatal) > 0 {
		return nil, fmt.Errorf(
			"package %s does not compile; fix it first:\n%s",
			l.pkg.PkgPath,
			strings.Join(fatal, "\n"),
		)
	}
	return l, nil
}

func (l *loaded) load(generated []byte) (*packages.Package, error) {
	cfg := &packages.Config{
		Mode:    loadMode,
		Dir:     l.dir,
		Fset:    token.NewFileSet(),
		Overlay: map[string][]byte{l.outPath: generated},
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("load package %s: %w", l.dir, err)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("%s: expected one package, found %d", l.dir, len(pkgs))
	}
	return pkgs[0], nil
}

// isCompileReport reports whether e is the go command's compile output for
// the package itself. go list compiles the package for export data and
// reports its type errors as one list error; the type check of the source
// reports the same errors one by one, with positions.
func isCompileReport(pkg *packages.Package, e packages.Error) bool {
	return e.Kind == packages.ListError && strings.HasPrefix(e.Msg, "# "+pkg.PkgPath+"\n")
}

// errorsWithin returns the type errors located in node, the declaration
// interface. Errors elsewhere may come from code that calls the generated
// API; verify checks them once the new output is in place.
func (l *loaded) errorsWithin(node ast.Node) []string {
	start, end := l.pkg.Fset.Position(node.Pos()), l.pkg.Fset.Position(node.End())
	var out []string
	for _, e := range l.pkg.Errors {
		if file, line := splitPos(e.Pos); file == start.Filename && line >= start.Line && line <= end.Line {
			out = append(out, l.rel(e.Error()))
		}
	}
	return out
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
func (l *loaded) lookup(name string) (types.Object, string) {
	obj := l.pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return nil, ""
	}
	return obj, l.position(obj.Pos())
}

func (l *loaded) position(p token.Pos) string {
	pos := l.pkg.Fset.Position(p)
	return fmt.Sprintf("%s:%d", l.relPath(pos.Filename), pos.Line)
}

func (l *loaded) relPath(name string) string {
	if rel, err := filepath.Rel(l.dir, name); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return name
}

// rel shortens absolute paths in a message to paths relative to the package.
func (l *loaded) rel(msg string) string {
	return strings.ReplaceAll(msg, l.dir+string(filepath.Separator), "")
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
	var genErrs, otherErrs []string
	for _, e := range pkg.Errors {
		if isCompileReport(pkg, e) {
			continue
		}
		file, line := splitPos(e.Pos)
		if file != l.outPath {
			otherErrs = append(otherErrs, l.rel(e.Error()))
			continue
		}
		at, name := c.at, c.iface
		for _, s := range starts {
			if s.line <= line {
				at, name = s.decl.at, s.decl.name
			}
		}
		genErrs = append(genErrs, fmt.Sprintf("%s: %s: generated code does not compile: %s (%s:%d)",
			at, name, e.Msg, out, line))
	}
	if len(genErrs)+len(otherErrs) == 0 {
		for _, e := range pkg.Errors {
			otherErrs = append(otherErrs, l.rel(e.Error()))
		}
	}
	if len(genErrs) > 0 {
		return fmt.Errorf("%s\nthis is a gap in the nano gen errors checks; rename the declaration to work "+
			"around it and report it", strings.Join(genErrs, "\n"))
	}
	return fmt.Errorf("package %s does not compile with the new %s; update the code that uses it:\n%s",
		pkg.PkgPath, out, strings.Join(otherErrs, "\n"))
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

// splitPos splits "file:line:col" into the file and the line.
func splitPos(pos string) (string, int) {
	parts := strings.Split(pos, ":")
	if len(parts) < 3 {
		return pos, 0
	}
	var line int
	_, _ = fmt.Sscanf(parts[len(parts)-2], "%d", &line)
	return strings.Join(parts[:len(parts)-2], ":"), line
}
