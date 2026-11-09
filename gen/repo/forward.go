package genrepo

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// Planned returns the files nano gen repo writes for the package in dir,
// whose declaring files are files: repo_base.gen.go, and one
// <file>_repo.gen.go per file that calls nanorepo.Implement. It reads the
// files by parsing alone, as the pipeline plans on a tree that may not
// compile.
func Planned(dir string, files []string) []string {
	out := []string{filepath.Join(dir, FileName)}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err == nil && callsImplement(f) {
			out = append(out, filepath.Join(dir, derivedFile(path)))
		}
	}
	return out
}

// ForwardStubs returns, for the package in dir, a stub of each
// <file>_repo.gen.go that nano gen repo would write, by absolute path, for
// the pipeline steps that run before repo (design D27). A stub declares a
// method that panics for each method of each bound interface that the
// implementation has no method of that name for, in a file nano gen repo
// did not write, and no var _ line. So a step before repo still type-checks
// a package whose interface just gained a method, which the body-stripped
// stub of the last output cannot do.
//
// It parses the package and type-checks nothing. An interface or an
// implementation it cannot read from the package's own files, such as an
// interface of another package, or a method of an interface embedded from
// another package, gets no stub method: the step then sees the package as
// the last run left it.
func ForwardStubs(dir string) map[string][]byte {
	p := parsePackage(dir)
	if p == nil {
		return nil
	}
	out := map[string][]byte{}
	for _, file := range p.declaring {
		if src := p.stub(file); src != nil {
			out[filepath.Join(dir, derivedFile(file.path))] = src
		}
	}
	return out
}

// parsedFile is one Go file of a package, parsed.
type parsedFile struct {
	path string
	f    *ast.File
}

// parsedPackage is what ForwardStubs reads from a package.
type parsedPackage struct {
	name       string
	fset       *token.FileSet
	ifaces     map[string]ifaceDecl       // interface type name -> declaration
	methods    map[string]map[string]bool // receiver type -> method names, outside nano gen repo files
	ptr, val   map[string]bool            // receiver type -> its methods use a pointer, a value receiver
	declaring  []parsedFile               // files with nanorepo.Implement calls
	implements map[string][][2]string     // declaring file -> (interface, implementation) names
}

// ifaceDecl is an interface type and the file it is declared in.
type ifaceDecl struct {
	t    *ast.InterfaceType
	file *ast.File
}

// parsePackage parses the non-test Go files of dir that the default build
// context selects, or returns nil when dir holds none.
func parsePackage(dir string) *parsedPackage {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	p := &parsedPackage{fset: token.NewFileSet(), ifaces: map[string]ifaceDecl{},
		methods: map[string]map[string]bool{}, ptr: map[string]bool{}, val: map[string]bool{},
		implements: map[string][][2]string{}}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if ok, matchErr := build.Default.MatchFile(dir, name); matchErr != nil || !ok {
			continue
		}
		path := filepath.Join(dir, name)
		f, parseErr := parser.ParseFile(p.fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseErr != nil {
			continue
		}
		p.name = f.Name.Name
		p.read(path, f)
	}
	if p.name == "" {
		return nil
	}
	return p
}

// read records the interfaces, methods and bindings of one file.
func (p *parsedPackage) read(path string, f *ast.File) {
	if ownRepo(f) {
		return
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				p.readType(f, spec)
			}
		case *ast.FuncDecl:
			p.readMethod(d)
		}
	}
	if pairs := implementCalls(f); len(pairs) > 0 {
		p.declaring = append(p.declaring, parsedFile{path: path, f: f})
		p.implements[path] = pairs
	}
}

// readType records an interface type of the package.
func (p *parsedPackage) readType(f *ast.File, spec ast.Spec) {
	ts, ok := spec.(*ast.TypeSpec)
	if !ok || ts.TypeParams != nil {
		return
	}
	if it, isIface := ts.Type.(*ast.InterfaceType); isIface {
		p.ifaces[ts.Name.Name] = ifaceDecl{t: it, file: f}
	}
}

// readMethod records a method and the receiver it takes.
func (p *parsedPackage) readMethod(d *ast.FuncDecl) {
	if d.Recv == nil || len(d.Recv.List) != 1 {
		return
	}
	recv, star := recvType(d.Recv.List[0].Type)
	if p.methods[recv] == nil {
		p.methods[recv] = map[string]bool{}
	}
	p.methods[recv][d.Name.Name] = true
	p.ptr[recv], p.val[recv] = p.ptr[recv] || star, p.val[recv] || !star
}

// ownRepo reports whether f starts with the header of nano gen repo.
func ownRepo(f *ast.File) bool {
	return len(f.Comments) > 0 && f.Comments[0].Pos() < f.Package &&
		strings.HasPrefix(f.Comments[0].List[0].Text, strings.TrimSpace(strings.SplitN(header, "\n", 2)[0]))
}

// callsImplement reports whether f calls nanorepo.Implement[...], whatever
// its type arguments: an interface of another package or an instantiated
// generic interface binds too.
func callsImplement(f *ast.File) bool {
	name := nanorepoName(f)
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if ix, ok := n.(*ast.IndexListExpr); ok && name != "" {
			sel, isSel := ix.X.(*ast.SelectorExpr)
			found = found || isSel && sel.Sel.Name == "Implement" && identOf(sel.X) != nil &&
				identOf(sel.X).Name == name
		}
		return !found
	})
	return found
}

// implementCalls returns the (interface, implementation) type names of the
// nanorepo.Implement[I, Impl](...) calls of f whose type arguments are
// names of the package, as parsing alone reads them.
func implementCalls(f *ast.File) [][2]string {
	name := nanorepoName(f)
	if name == "" {
		return nil
	}
	var out [][2]string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ix, ok := call.Fun.(*ast.IndexListExpr)
		if !ok || len(ix.Indices) != 2 {
			return true
		}
		sel, ok := ix.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Implement" || identOf(sel.X) == nil || identOf(sel.X).Name != name {
			return true
		}
		iface, impl := identOf(ix.Indices[0]), identOf(ix.Indices[1])
		if iface != nil && impl != nil {
			out = append(out, [2]string{iface.Name, impl.Name})
		}
		return true
	})
	return out
}

// nanorepoName returns the name f imports db/nanorepo under, or "".
func nanorepoName(f *ast.File) string {
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == nanorepoPath {
			name := "nanorepo"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "_" || name == "." {
				return ""
			}
			return name
		}
	}
	return ""
}

// stubMethod is one method a stub declares.
type stubMethod struct {
	name string
	ft   *ast.FuncType
	file *ast.File // where the interface declares it, for its imports
}

// stub renders the forward stub of one declaring file, or nil when no
// binding of it lacks a method.
func (p *parsedPackage) stub(file parsedFile) []byte {
	var body bytes.Buffer
	imports := map[string]string{} // name -> path
	for _, pair := range p.implements[file.path] {
		recv := "r " + pair[1]
		if p.ptr[pair[1]] || !p.val[pair[1]] {
			recv = "r *" + pair[1]
		}
		done := map[string]bool{}
		for _, m := range p.interfaceMethods(pair[0], map[string]bool{}) {
			if p.methods[pair[1]][m.name] || done[m.name] {
				continue
			}
			done[m.name] = true
			sig, ok := p.signature(m, imports)
			if !ok {
				continue
			}
			body.WriteString("func (" + recv + ") " + m.name + sig + " { panic(\"nano gen stub\") }\n\n")
		}
	}
	if body.Len() == 0 {
		return nil
	}
	var out bytes.Buffer
	out.WriteString(header + "package " + p.name + "\n\n")
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		out.WriteString("import " + name + " " + strconv.Quote(imports[name]) + "\n")
	}
	out.WriteString("\n")
	out.Write(body.Bytes())
	return out.Bytes()
}

// interfaceMethods returns the methods of the interface name, with those of
// the interfaces of the package it embeds, each name once.
func (p *parsedPackage) interfaceMethods(name string, seen map[string]bool) []stubMethod {
	decl, ok := p.ifaces[name]
	if !ok || seen[name] {
		return nil
	}
	seen[name] = true
	var out []stubMethod
	for _, field := range decl.t.Methods.List {
		ft, isFunc := field.Type.(*ast.FuncType)
		switch {
		case isFunc && len(field.Names) == 1:
			out = append(out, stubMethod{name: field.Names[0].Name, ft: ft, file: decl.file})
		case len(field.Names) == 0 && identOf(field.Type) != nil:
			out = append(out, p.interfaceMethods(identOf(field.Type).Name, seen)...)
		}
	}
	return out
}

// signature prints the parameters and results of m, and adds the imports
// they use. ok is false when a package they name has no import in the
// interface's file.
func (p *parsedPackage) signature(m stubMethod, imports map[string]string) (string, bool) {
	byName := map[string]string{}
	for _, imp := range m.file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil {
			byName[gopkg.ImportName(imp)] = path
		}
	}
	ok := true
	used := map[string]string{}
	ast.Inspect(m.ft, func(n ast.Node) bool {
		sel, isSel := n.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		if x := identOf(sel.X); x != nil {
			path, found := byName[x.Name]
			ok = ok && found
			used[x.Name] = path
		}
		return true
	})
	var b bytes.Buffer
	if err := printer.Fprint(&b, p.fset, m.ft); err != nil || !ok {
		return "", false
	}
	maps.Copy(imports, used)
	return strings.TrimPrefix(b.String(), "func"), true
}
