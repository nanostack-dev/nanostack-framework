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
// <file>_repo.gen.go per file that calls nanorepo.Implement or
// nanorepo.Search. It reads the files by parsing alone, as the pipeline
// plans on a tree that may not compile.
func Planned(dir string, files []string) []string {
	out := []string{filepath.Join(dir, FileName)}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err == nil && (callsImplement(f) || callsSearch(f)) {
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
// a package whose interface just gained a method, or whose implementation
// just got a hand-written method that the last run derived: the
// body-stripped stub of the last output can do neither. A declaring file
// whose bindings lack no method gets a stub with no method.
//
// The methods come from parsing alone. The methods of an interface it
// cannot read from the package's own files, an interface of another package
// or one embedded from another package, are taken from the last output on
// disk, minus those the implementation now has: the step sees them as the
// last run left them.
//
// A file that declares searches also gets their builders (design S10): the
// scope type first, then the builder, its sort enum, its spec and its
// accessors, as a run writes them. They come from the declarations and the
// types of the packages they import; the hand-written code that uses the
// builders need not type-check. A search that nano gen repo cannot write
// gets its scope type only, so a From function that takes it still
// type-checks. When the declarations have a problem, the stub has no
// builder, and the repo step reports the problem.
func ForwardStubs(dir string) map[string][]byte {
	p := parsePackage(dir)
	if p == nil {
		return nil
	}
	var typed *stubSearches
	if len(p.searching) > 0 {
		typed = typedSearches(dir)
	}
	out := typed.mappers()
	if out == nil {
		out = map[string][]byte{}
	}
	for _, file := range p.declaring {
		path := filepath.Join(dir, derivedFile(file.path))
		out[path] = p.stub(file, p.own[path], typed)
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
	declaring  []parsedFile               // files with nanorepo.Implement or nanorepo.Search calls
	searching  map[string]bool            // declaring file -> it calls nanorepo.Search
	implements map[string][][2]string     // declaring file -> (interface, implementation) names; "" for an interface of another package
	own        map[string]*ast.File       // path -> a file nano gen repo wrote
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
		implements: map[string][][2]string{}, own: map[string]*ast.File{}, searching: map[string]bool{}}
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
		p.own[path] = f
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
	pairs, searches := implementCalls(f), callsSearch(f)
	if len(pairs) > 0 || searches {
		p.declaring = append(p.declaring, parsedFile{path: path, f: f})
		p.implements[path] = pairs
		p.searching[path] = searches
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

// callsSearch reports whether f calls nanorepo.Search[...].
func callsSearch(f *ast.File) bool {
	name := nanorepoName(f)
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if ix, ok := n.(*ast.IndexExpr); ok && name != "" {
			sel, isSel := ix.X.(*ast.SelectorExpr)
			found = found || isSel && sel.Sel.Name == "Search" && identOf(sel.X) != nil &&
				identOf(sel.X).Name == name
		}
		return !found
	})
	return found
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
// nanorepo.Implement[I, Impl](...) calls of f whose implementation is a
// name of the package, as parsing alone reads them. The interface is ""
// when it is not a name of the package, such as pkg.Repository or
// Repository[Item].
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
		if impl := identOf(ix.Indices[1]); impl != nil {
			iface := ""
			if id := identOf(ix.Indices[0]); id != nil {
				iface = id.Name
			}
			out = append(out, [2]string{iface, impl.Name})
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

// stub renders the forward stub of one declaring file. last is the derived
// file on disk, nil when there is none: the methods of an interface the
// package does not declare are taken from it. typed holds the builders of
// the package's searches, nil when it has none or they do not read.
func (p *parsedPackage) stub(file parsedFile, last *ast.File, typed *stubSearches) []byte {
	var body bytes.Buffer
	imports := map[string]string{} // name -> path
	for _, pair := range p.implements[file.path] {
		recv := "r " + pair[1]
		if p.ptr[pair[1]] || !p.val[pair[1]] {
			recv = "r *" + pair[1]
		}
		methods, complete := p.interfaceMethods(pair[0], map[string]bool{})
		if !complete {
			methods = append(methods, lastMethods(last, pair[1])...)
		}
		done := map[string]bool{}
		for _, m := range methods {
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
	if searches := typed.of(file.path); len(searches) > 0 {
		if src, ok := typed.stub(p.name, imports, body.Bytes(), searches); ok {
			return src
		}
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
// the interfaces of the package it embeds. complete is false when the
// package does not declare name or an interface it embeds.
func (p *parsedPackage) interfaceMethods(name string, seen map[string]bool) (out []stubMethod, complete bool) {
	if seen[name] {
		return nil, true
	}
	decl, ok := p.ifaces[name]
	if !ok {
		return nil, false
	}
	seen[name] = true
	complete = true
	for _, field := range decl.t.Methods.List {
		ft, isFunc := field.Type.(*ast.FuncType)
		switch {
		case isFunc && len(field.Names) == 1:
			out = append(out, stubMethod{name: field.Names[0].Name, ft: ft, file: decl.file})
		case len(field.Names) == 0 && identOf(field.Type) != nil:
			more, all := p.interfaceMethods(identOf(field.Type).Name, seen)
			out, complete = append(out, more...), complete && all
		default:
			complete = false // embedded from another package, or a type union
		}
	}
	return out, complete
}

// lastMethods returns the methods on impl of last, the derived file the
// last run wrote, nil when there is none.
func lastMethods(last *ast.File, impl string) []stubMethod {
	if last == nil {
		return nil
	}
	var out []stubMethod
	for _, decl := range last.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		if recv, _ := recvType(fd.Recv.List[0].Type); recv == impl {
			out = append(out, stubMethod{name: fd.Name.Name, ft: fd.Type, file: last})
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
