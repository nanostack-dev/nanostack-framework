package genrepo

import (
	"bytes"
	"cmp"
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

const loadMode = packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
	packages.NeedFiles | packages.NeedImports

// compileFix is the fix of a compile error in the declaring package.
const compileFix = "fix the compile error here; nano gen repo needs the package to type-check, " +
	"apart from the names it generates"

// loaded is the declaring package, type-checked with the files nano gen
// repo wrote replaced by empty files.
type loaded struct {
	dir     string // absolute package directory
	outPath string // absolute path of repo_base.gen.go
	overlay map[string][]byte
	empty   []byte   // an empty file of the package
	own     []string // absolute paths of the files nano gen repo wrote, by their header
	oldBase []byte   // repo_base.gen.go before this run, for the columns it had
	pkg     *packages.Package
	files   map[string]map[string]*ast.File // package path -> file name -> parsed file, for tables and enums
	fset    *token.FileSet                  // of files
	typed   map[string]*types.Package       // package path -> types, for the models of searched tables
	scopes  []string                        // the tenant columns, Postgres.Scopes
}

// loadPackage type-checks the package with repo_base.gen.go and every
// other file nano gen repo wrote replaced by empty files, so a stale or
// broken previous output never blocks a run, and a method a previous run
// generated never counts as hand-written. Hand-written code that uses the
// base does not type-check yet; verify reports what remains once the new
// outputs are in place. Errors that stop type checking (go list, syntax)
// are returned.
func loadPackage(opts Options) (*loaded, error) {
	absDir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, gen.EnvErrorf("run from a directory that exists", "resolve %s: %v", opts.Dir, err)
	}
	pkgName, err := gopkg.PackageName(absDir, FileName)
	if err != nil {
		return nil, err
	}
	l := &loaded{dir: absDir, outPath: filepath.Join(absDir, FileName), overlay: opts.Overlay,
		empty: []byte("package " + pkgName + "\n")}
	l.findOwn()
	l.pkg, err = l.load(nil)
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
			"make the package compile; nano gen repo replaces the files it wrote with empty files")...)
	}
	return l, nil
}

// findOwn lists the Go files of the package directory, on disk or in the
// overlay, that start with the header of nano gen repo, and keeps the
// base on disk.
func (l *loaded) findOwn() {
	names := map[string]bool{}
	if entries, err := os.ReadDir(l.dir); err == nil {
		for _, e := range entries {
			names[filepath.Join(l.dir, e.Name())] = true
		}
	}
	for path := range l.overlay {
		if filepath.Dir(path) == l.dir {
			names[path] = true
		}
	}
	for path := range names {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, ok := l.overlay[path]
		if !ok {
			var err error
			if data, err = os.ReadFile(filepath.Clean(path)); err != nil {
				continue
			}
		}
		if bytes.HasPrefix(data, []byte(header)) {
			l.own = append(l.own, path)
			if path == l.outPath {
				l.oldBase = data
			}
		}
	}
	slices.Sort(l.own)
}

// isOwn reports whether path is a file nano gen repo wrote.
func (l *loaded) isOwn(path string) bool {
	_, found := slices.BinarySearch(l.own, path)
	return found
}

// load type-checks the package with generated in place of the files nano
// gen repo writes, and every other file it wrote empty, on top of the
// caller's overlay.
func (l *loaded) load(generated []gen.File) (*packages.Package, error) {
	own := map[string][]byte{l.outPath: l.empty}
	for _, path := range l.own {
		own[path] = l.empty
	}
	for _, f := range generated {
		own[f.Path] = f.Data
	}
	overlay, err := gopkg.Overlay(l.overlay, own)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{Mode: loadMode, Dir: l.dir, Fset: token.NewFileSet(), Overlay: overlay}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, gopkg.LoadFailed(l.dir, err)
	}
	if len(pkgs) != 1 {
		return nil, gen.Errorf("", 0, "pass -dir with the directory of one package",
			"%s holds %d packages", l.dir, len(pkgs))
	}
	return pkgs[0], nil
}

// errorsWithin returns the type errors of pkg located in node, as problems.
func errorsWithin(pkg *packages.Package, node ast.Node, rel func(string) string, fix string) []error {
	start, end := pkg.Fset.Position(node.Pos()), pkg.Fset.Position(node.End())
	var within []packages.Error
	for _, e := range pkg.Errors {
		if file, line := gopkg.SplitPos(e.Pos); file == start.Filename && line >= start.Line && line <= end.Line {
			within = append(within, e)
		}
	}
	return gopkg.Problems(within, rel, fix)
}

// position returns the file, relative to the package, and the line of p.
func (l *loaded) position(fset *token.FileSet, p token.Pos) (string, int) {
	at := fset.Position(p)
	return l.relPath(at.Filename), at.Line
}

func (l *loaded) relPath(name string) string {
	if rel, err := filepath.Rel(l.dir, name); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return name
}

// own reports whether f is a file that nano gen wrote: its header says
// "Code generated by nano gen". A file another tool generated, such as a
// mock, is hand-written as far as the package is concerned.
func own(f *ast.File) bool {
	if !ast.IsGenerated(f) {
		return false
	}
	for _, cg := range f.Comments {
		if cg.Pos() >= f.Package {
			break
		}
		if strings.Contains(cg.Text(), "Code generated by nano gen ") {
			return true
		}
	}
	return false
}

// failedImport returns the import path of a "could not import" error of
// pkg, and the reason as ": reason": the go command's error on the imported
// package when there is one, such as "no required module provides
// package", else the type checker's.
func failedImport(pkg *packages.Package, msg string) (path, reason string, ok bool) {
	rest, found := strings.CutPrefix(msg, "could not import ")
	if !found {
		return "", "", false
	}
	path, reason, _ = strings.Cut(rest, " ")
	reason = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(reason), "("), ")")
	if imp := pkg.Imports[path]; imp != nil && len(imp.Errors) > 0 {
		reason, _, _ = strings.Cut(imp.Errors[0].Msg, "\n")
		reason, _, _ = strings.Cut(reason, "; to add it") // the fix says it
	}
	if reason != "" {
		reason = ": " + reason
	}
	return path, reason, path != ""
}

// rendered is what one run writes: the base, then one file of derived
// methods per declaring file with a binding.
type rendered struct {
	files    []gen.File
	byFile   map[string][]*resolution // derived file name -> its bindings, in file order
	searches map[string][]*searchDecl // derived file name -> its searches, in file order
}

// verify type-checks the package with the rendered files and returns it.
// An import of a generated file that the module cannot load is a missing
// requirement in go.mod, such as pkg/db/transactor in a module that never
// imported it. Any other type error in a generated file is a gap in the
// checks of this package. Both are reported at the declaration that
// produced the line. Any other type error is the package's own, reported at
// its line: nano gen repo writes nothing while the package does not compile
// with what it would write.
//
// failing is true when resolution already failed: the generated files then
// lack the methods that failed, so their errors, and a missing method in
// hand-written code, repeat a reported problem and are left out.
func (l *loaded) verify(decls *declarations, out *rendered, failing bool, missing []string) (
	*packages.Package, []error, error,
) {
	pkg, err := l.load(out.files)
	if err != nil {
		return nil, nil, err
	}
	srcs := map[string][]byte{}
	for _, f := range out.files {
		srcs[f.Path] = f.Data
	}
	var other []packages.Error
	var imports, gaps []error
	for _, e := range pkg.Errors {
		if gopkg.IsCompileReport(pkg, e) {
			continue
		}
		file, line := gopkg.SplitPos(e.Pos)
		src, generated := srcs[file]
		switch {
		case !generated:
			if !failing || !consequence(e.Msg, missing) {
				other = append(other, e)
			}
		case failing:
			if path, reason, ok := failedImport(pkg, e.Msg); ok {
				imports = append(imports, l.importProblem(decls, out, file, src, line, path, reason))
			}
		default:
			if path, reason, ok := failedImport(pkg, e.Msg); ok {
				imports = append(imports, l.importProblem(decls, out, file, src, line, path, reason))
				continue
			}
			gaps = append(gaps, l.gap(decls, out, file, src, line, e.Msg))
		}
	}
	if len(imports) > 0 {
		return nil, imports, nil
	}
	if len(gaps) > 0 {
		return nil, gaps, nil
	}
	slices.SortStableFunc(other, func(a, b packages.Error) int {
		fa, la := gopkg.SplitPos(a.Pos)
		fb, lb := gopkg.SplitPos(b.Pos)
		return cmp.Or(strings.Compare(fa, fb), cmp.Compare(la, lb))
	})
	problems := make([]error, 0, len(other))
	for _, e := range other {
		file, line := gopkg.SplitPos(e.Pos)
		problems = append(problems, gen.Errorf(l.relPath(file), line, compileFix, "compile error: %s", e.Msg))
	}
	return pkg, problems, nil
}

// consequence reports whether a compile error in hand-written code follows
// from a method that failed to resolve.
func consequence(msg string, missing []string) bool {
	if strings.Contains(msg, "missing method") {
		return true
	}
	for _, m := range missing {
		if strings.HasPrefix(m, "undefined: ") && strings.HasSuffix(msg, m) {
			return true
		}
		if strings.HasSuffix(msg, "has no field or method "+m+")") {
			return true
		}
	}
	return false
}

// origin returns the declaration that produced line of the generated file:
// the table whose base holds it, or the binding whose code holds it.
func (l *loaded) origin(decls *declarations, out *rendered, file string, src []byte, line int) at {
	if file == l.outPath {
		t := decls.tableAtLine(src, line)
		return at{t.file, t.line}
	}
	res := out.byFile[filepath.Base(file)]
	if s := searchAtLine(src, line, out.searches[filepath.Base(file)], len(res)); s != nil {
		return s.at
	}
	r := bindingAtLine(src, line, res, func(r *resolution) string { return r.explain.Iface })
	return at{r.b.file, r.b.line}
}

// importProblem reports an import of a generated file that the module
// cannot load.
func (l *loaded) importProblem(decls *declarations, out *rendered, file string, src []byte, line int,
	path, reason string,
) error {
	o := l.origin(decls, out, file, src, line)
	return gen.Errorf(o.file, o.line, "add its module to go.mod: go get "+path+", then rerun nano gen repo",
		"the generated %s imports %s, which the module cannot load%s", filepath.Base(file), path, reason)
}

// gap reports a type error in a generated file that the checks of nano gen
// repo let through.
func (l *loaded) gap(decls *declarations, out *rendered, file string, src []byte, line int, msg string) error {
	o := l.origin(decls, out, file, src, line)
	if file == l.outPath {
		t := decls.tableAtLine(src, line)
		return gen.Errorf(o.file, o.line,
			"report this gap in the nano gen repo checks; meanwhile declare the table with nanorepo.Base "+
				"or write the repository by hand",
			"the base of table %s does not compile: %s (%s:%d)", t.sqlName, msg, FileName, line)
	}
	fix := "report this gap in the nano gen repo checks; meanwhile write the method by hand"
	if searchAtLine(src, line, out.searches[filepath.Base(file)], len(out.byFile[filepath.Base(file)])) != nil {
		fix = "report this gap in the nano gen repo checks; meanwhile write the search by hand on " +
			"searchexec.DefineSpec"
	}
	return gen.Errorf(o.file, o.line, fix,
		"the generated %s does not compile: %s (%s:%d)", filepath.Base(file), msg, filepath.Base(file), line)
}

// typesOf returns the type-checked package at path: from the imports of the
// declaring package when it imports it, directly or not, else loaded on its
// own. It returns nil, nil when no package is at path.
func (l *loaded) typesOf(path string) (*types.Package, error) {
	if pkg, ok := l.typed[path]; ok {
		return pkg, nil
	}
	if l.typed == nil {
		l.typed = map[string]*types.Package{}
	}
	seen := map[*types.Package]bool{}
	var find func(p *types.Package) *types.Package
	find = func(p *types.Package) *types.Package {
		if seen[p] {
			return nil
		}
		seen[p] = true
		if p.Path() == path {
			return p
		}
		for _, imp := range p.Imports() {
			if found := find(imp); found != nil {
				return found
			}
		}
		return nil
	}
	if found := find(l.pkg.Types); found != nil {
		l.typed[path] = found
		return found, nil
	}
	overlay, err := gopkg.Overlay(l.overlay, nil)
	if err != nil {
		return nil, err
	}
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes, Dir: l.dir,
		Overlay: overlay}, path)
	if err != nil {
		return nil, gopkg.LoadFailed(l.dir, err)
	}
	var pkg *types.Package
	if len(pkgs) == 1 && len(pkgs[0].Errors) == 0 {
		pkg = pkgs[0].Types
	}
	l.typed[path] = pkg
	return pkg, nil
}
