package genrepo

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// apiHome is the package of the handler that serves the request body of a
// search, when it is not the declaring package: the API mapper goes there,
// next to the handler, in <search>_search_api.gen.go (design S10, approved
// answer 8).
type apiHome struct {
	dir     string // absolute
	pkgName string
	pkgPath string
	file    string // absolute path of the mapper file
}

// mapperName is the API mapper of s.
func mapperName(s *searchDecl) string { return s.name + "SearchFromAPI" }

// mapperFile is the file of the API mapper of s in another package.
func mapperFile(s *searchDecl) string { return snake(s.name) + "_search_api.gen.go" }

// apiHome finds the handler of each operation whose request object holds
// body: a method <Operation>(ctx, <Operation>RequestObject) in a Go file of
// the module. A handler in the declaring package, or no handler yet, keeps
// the mapper in the declaring file (nil). A handler in one other package
// moves it there. The module is the directory tree of the nearest go.mod
// above the package, without vendor, hidden and underscore directories,
// nested modules, and testdata directories that do not hold the package.
func (l *loaded) apiHome(s *searchDecl, body *types.Named) (*apiHome, error) {
	ops := requestObjects(body)
	if len(ops) == 0 {
		return nil, nil
	}
	root, modPath := moduleOf(l.dir)
	if root == "" {
		return nil, nil
	}
	homes := l.handlerDirs(root, ops)
	keys := make([]string, 0, len(homes))
	for dir := range homes {
		keys = append(keys, dir)
	}
	slices.Sort(keys)
	switch {
	case len(keys) == 0:
		return nil, nil
	case len(keys) > 1:
		rel := make([]string, len(keys))
		for i, k := range keys {
			rel[i] = l.relDir(k)
		}
		return nil, gen.Errorf(s.at.file, s.at.line, "serve the operations of "+body.Obj().Name()+" from one package",
			"search %s: the handlers of %s are in %s, and the API mapper can go next to one of them only",
			s.name, strings.Join(ops, ", "), strings.Join(rel, " and "))
	case keys[0] == l.dir:
		return nil, nil
	}
	rel, err := filepath.Rel(root, keys[0])
	if err != nil {
		return nil, nil //nolint:nilerr // a handler outside the module root is not a home
	}
	home := &apiHome{dir: keys[0], pkgName: homes[keys[0]], pkgPath: path.Join(modPath, filepath.ToSlash(rel)),
		file: filepath.Join(keys[0], mapperFile(s))}
	if home.pkgPath == body.Obj().Pkg().Path() || l.imports(home.pkgPath) {
		return nil, gen.Errorf(s.at.file, s.at.line, "move the handler of "+strings.Join(ops, ", ")+
			" to a package that the declaring package does not import",
			"search %s: the handler is in %s, which package %s imports, so the API mapper there would make an "+
				"import cycle", s.name, home.pkgPath, l.pkg.Name)
	}
	if file, line, found := l.declaredIn(home.dir, mapperName(s)); found {
		return nil, gen.Errorf(l.relDir(file), line, "rename it, or rename the search",
			"%s is declared here, and nanostack gen repo declares it for search %s", mapperName(s), s.name)
	}
	return home, nil
}

// declaredIn returns where a hand-written file of the package in dir
// declares the package-level function name.
func (l *loaded) declaredIn(dir, name string) (file string, line int, found bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, false
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, ok := l.source(p)
		if !ok || bytes.HasPrefix(data, []byte(header)) || !bytes.Contains(data, []byte(name)) {
			continue
		}
		f, parseErr := parser.ParseFile(fset, p, data, parser.SkipObjectResolution)
		if parseErr != nil {
			continue
		}
		for _, decl := range f.Decls {
			if fd, isFunc := decl.(*ast.FuncDecl); isFunc && fd.Recv == nil && fd.Name.Name == name {
				return p, fset.Position(fd.Pos()).Line, true
			}
		}
	}
	return "", 0, false
}

// source returns the file at path from the overlay, or from the disk. ok
// is false for a file that cannot be read: it holds nothing the run sees.
func (l *loaded) source(path string) (data []byte, ok bool) {
	if data, ok = l.overlay[path]; ok {
		return data, true
	}
	data, err := os.ReadFile(filepath.Clean(path))
	return data, err == nil
}

// relDir names a directory relative to the package, for a message.
func (l *loaded) relDir(dir string) string {
	if rel, err := filepath.Rel(l.dir, dir); err == nil {
		return rel
	}
	return dir
}

// imports reports whether the declaring package imports the package at
// pkgPath, directly or not.
func (l *loaded) imports(pkgPath string) bool {
	seen := map[*types.Package]bool{}
	var walk func(p *types.Package) bool
	walk = func(p *types.Package) bool {
		if seen[p] {
			return false
		}
		seen[p] = true
		for _, imp := range p.Imports() {
			if imp.Path() == pkgPath || walk(imp) {
				return true
			}
		}
		return false
	}
	return walk(l.pkg.Types)
}

// requestObjects returns the operations whose strict-server request object
// holds body, in the package of body: the types <Operation>RequestObject
// whose Body field is a *body. Their names are the handler methods.
func requestObjects(body *types.Named) []string {
	pkg := body.Obj().Pkg()
	if pkg == nil {
		return nil
	}
	var ops []string
	for _, name := range pkg.Scope().Names() {
		op, ok := strings.CutSuffix(name, "RequestObject")
		obj, isType := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || !isType || op == "" {
			continue
		}
		st, isStruct := obj.Type().Underlying().(*types.Struct)
		if !isStruct {
			continue
		}
		for f := range st.Fields() {
			if f.Name() == "Body" && types.Identical(deref(f.Type()), body) {
				ops = append(ops, op)
			}
		}
	}
	return ops
}

// moduleOf returns the directory of the nearest go.mod above dir and its
// module path, or "" when there is none.
func moduleOf(dir string) (root, modPath string) {
	for d := dir; ; d = filepath.Dir(d) {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			return d, modulePath(data)
		}
		if filepath.Dir(d) == d {
			return "", ""
		}
	}
}

// modulePath reads the module directive of a go.mod file.
func modulePath(data []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// handlerDirs returns the directories under root that declare a handler
// of one of ops, with their package names. Files are parsed only when they
// name a request object of ops.
func (l *loaded) handlerDirs(root string, ops []string) map[string]string {
	dirs := map[string]string{}
	fset := token.NewFileSet()
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable directory holds no handler that the run can see
		}
		if e.IsDir() {
			if p != root && l.skipDir(p, e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		data, ok := l.source(p)
		names := func(op string) bool { return bytes.Contains(data, []byte(op+"RequestObject")) }
		if !ok || !slices.ContainsFunc(ops, names) {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, p, data, parser.SkipObjectResolution)
		if parseErr == nil && declaresHandler(f, ops) {
			dirs[filepath.Dir(p)] = f.Name.Name
		}
		return nil
	})
	return dirs
}

// skipDir reports whether the handler search skips a directory below the
// module root.
func (l *loaded) skipDir(p, name string) bool {
	switch {
	case name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_"):
		return true
	case name == "testdata":
		return !strings.HasPrefix(l.dir+string(filepath.Separator), p+string(filepath.Separator))
	}
	_, err := os.Stat(filepath.Join(p, "go.mod"))
	return err == nil
}

// declaresHandler reports whether f declares a method <op>(ctx,
// <op>RequestObject) for one of ops.
func declaresHandler(f *ast.File, ops []string) bool {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || !slices.Contains(ops, fd.Name.Name) || fd.Type.Params.NumFields() != 2 {
			continue
		}
		params := fd.Type.Params.List
		last := params[len(params)-1].Type
		if sel, isSel := last.(*ast.SelectorExpr); isSel {
			last = sel.Sel
		}
		if id, isIdent := last.(*ast.Ident); isIdent && id.Name == fd.Name.Name+"RequestObject" {
			return true
		}
	}
	return false
}
