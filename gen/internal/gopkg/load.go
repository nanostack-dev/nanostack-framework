package gopkg

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

// Load is packages.Load for a load with an overlay.
//
// go/packages treats any overlay as a reason to distrust export data
// (exportDataInvalid in go/packages), so it parses and type-checks every
// transitive dependency from source: the standard library and every module
// the package imports, once per load. On a service that is gigabytes of
// syntax trees and minutes of CPU for each of the pipeline's loads.
//
// The distrust is broader than it needs to be: go list builds export data
// with the overlay applied, so the export data of each package reflects the
// overlaid files it compiles. Load asks go list for that export data, then
// type-checks from source only the root packages (when the mode asks for
// syntax or type info) and any package whose export data is missing because
// it failed to compile. Everything else is read from export data, as
// go/packages does for a load without an overlay.
//
// A load without an overlay, without types, or with NeedDeps goes to
// packages.Load unchanged.
func Load(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
	const typed = packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax
	if len(cfg.Overlay) == 0 || cfg.Mode&typed == 0 || cfg.Mode&packages.NeedDeps != 0 {
		return packages.Load(cfg, patterns...)
	}
	list := *cfg
	list.Mode = cfg.Mode&^typed | packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
		packages.NeedImports | packages.NeedDeps | packages.NeedExportFile | packages.NeedModule |
		packages.NeedTypesSizes
	roots, err := packages.Load(&list, patterns...)
	if err != nil {
		return nil, err
	}
	fset := cfg.Fset
	if fset == nil {
		fset = token.NewFileSet()
	}
	ld := &loader{
		fset:    fset,
		overlay: cfg.Overlay,
		source:  cfg.Mode&(packages.NeedSyntax|packages.NeedTypesInfo) != 0,
		need:    map[*packages.Package]need{},
		done:    map[*packages.Package]bool{},
	}
	ld.plan(roots)
	for _, p := range roots {
		ld.load(p)
	}
	return roots, nil
}

// need says how a package gets its types.
type need int

const (
	needNone   need = iota // an indirect dependency: a placeholder that export data fills
	needExport             // read from its export data
	needSource             // parsed and type-checked from source
)

type loader struct {
	fset    *token.FileSet
	overlay map[string][]byte
	source  bool // roots are type-checked from source
	need    map[*packages.Package]need
	done    map[*packages.Package]bool
}

// plan gives every package a placeholder types.Package, so all export data
// readers share one object per path, and decides how each gets its types:
// roots from source (or export data when the mode needs no syntax), the
// direct imports of a source package from export data, and a package without
// export data from source.
func (ld *loader) plan(roots []*packages.Package) {
	packages.Visit(roots, nil, func(p *packages.Package) {
		if p.PkgPath == "unsafe" {
			p.Types = types.Unsafe
			return
		}
		p.Types = types.NewPackage(p.PkgPath, p.Name)
		p.Fset = ld.fset
	})
	var mark func(p *packages.Package, n need)
	mark = func(p *packages.Package, n need) {
		if p.PkgPath == "unsafe" {
			return
		}
		if n == needExport && p.ExportFile == "" {
			n = needSource
		}
		if ld.need[p] >= n {
			return
		}
		ld.need[p] = n
		if n == needSource {
			for _, imp := range p.Imports {
				mark(imp, needExport)
			}
		}
	}
	for _, p := range roots {
		if ld.source {
			mark(p, needSource)
		} else {
			mark(p, needExport)
		}
	}
}

// load gives p its types after every package it imports, so export data
// readers find the complete types of what they reference and never replace
// a package another reader already holds.
func (ld *loader) load(p *packages.Package) {
	if ld.done[p] {
		return
	}
	ld.done[p] = true
	for _, imp := range p.Imports {
		ld.load(imp)
	}
	switch ld.need[p] {
	case needExport:
		if err := ld.readExport(p); err != nil {
			p.Errors = append(p.Errors, packages.Error{Pos: "-", Msg: err.Error(), Kind: packages.UnknownError})
			p.IllTyped = true
		}
	case needSource:
		ld.check(p)
	case needNone:
	}
}

// readExport reads p's export data, with the types of p's transitive imports
// in view so the reader links to them instead of making copies.
func (ld *loader) readExport(p *packages.Package) error {
	f, err := os.Open(p.ExportFile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// go list -export names the compiler's archive, which NewReader unwraps,
	// as go/packages does. Go 1.29 drops archive support from gcexportdata;
	// by then go list must name cmd/export files, which Read takes directly.
	r, err := gcexportdata.NewReader(f) //nolint:staticcheck // go list -export still names an archive
	if err != nil {
		return fmt.Errorf("read export data of %s: %w", p.PkgPath, err)
	}
	view := map[string]*types.Package{}
	seen := map[*packages.Package]bool{}
	var visit func(imports map[string]*packages.Package)
	visit = func(imports map[string]*packages.Package) {
		for _, imp := range imports {
			if !seen[imp] {
				seen[imp] = true
				view[imp.PkgPath] = imp.Types
				visit(imp.Imports)
			}
		}
	}
	visit(p.Imports)
	tpkg, err := gcexportdata.Read(r, ld.fset, view, p.PkgPath)
	if err != nil {
		return fmt.Errorf("read export data of %s: %w", p.PkgPath, err)
	}
	p.Types = tpkg
	return nil
}

// check parses and type-checks p from source, reading overlaid files from
// the overlay, the way go/packages does for a root package.
func (ld *loader) check(p *packages.Package) {
	p.Syntax = ld.parse(p)
	p.TypesInfo = &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	tc := &types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) { return ld.importOf(p, path) }),
		Error:    func(err error) { addError(p, err) },
		Sizes:    p.TypesSizes,
	}
	if p.Module != nil && p.Module.GoVersion != "" {
		tc.GoVersion = "go" + p.Module.GoVersion
	}
	_ = types.NewChecker(tc, ld.fset, p.Types, p.TypesInfo).Files(p.Syntax)
	p.IllTyped = len(p.Errors) > 0
	for _, imp := range p.Imports {
		p.IllTyped = p.IllTyped || imp.IllTyped
	}
}

// parse parses the files p compiles, taking an overlaid file's content from
// the overlay.
func (ld *loader) parse(p *packages.Package) []*ast.File {
	files := make([]*ast.File, 0, len(p.CompiledGoFiles))
	for _, name := range p.CompiledGoFiles {
		src, ok := ld.overlay[filepath.Clean(name)]
		if !ok {
			var err error
			if src, err = os.ReadFile(filepath.Clean(name)); err != nil {
				addError(p, err)
				continue
			}
		}
		f, err := parser.ParseFile(ld.fset, name, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			addError(p, err)
		}
		if f != nil {
			files = append(files, f)
		}
	}
	return files
}

// importOf resolves an import of p, as written in p's source, to the types
// planned for it.
func (ld *loader) importOf(p *packages.Package, path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	imp, ok := p.Imports[path]
	if !ok {
		return nil, fmt.Errorf("no metadata for %s", path)
	}
	if imp.Types == nil || ld.need[imp] == needNone {
		return nil, fmt.Errorf("no type information for %s", path)
	}
	return imp.Types, nil
}

// addError records err on p the way go/packages converts the errors of a
// parse and type check.
func addError(p *packages.Package, err error) {
	var se scanner.ErrorList
	var te types.Error
	var pe *os.PathError
	switch {
	case errors.As(err, &se):
		for _, e := range se {
			p.Errors = append(p.Errors, packages.Error{Pos: e.Pos.String(), Msg: e.Msg, Kind: packages.ParseError})
		}
	case errors.As(err, &te):
		p.TypeErrors = append(p.TypeErrors, te)
		p.Errors = append(p.Errors, packages.Error{
			Pos: te.Fset.Position(te.Pos).String(), Msg: te.Msg, Kind: packages.TypeError,
		})
	case errors.As(err, &pe):
		p.Errors = append(p.Errors, packages.Error{
			Pos: pe.Path + ":1", Msg: pe.Err.Error(), Kind: packages.ParseError,
		})
	default:
		p.Errors = append(p.Errors, packages.Error{Pos: "-", Msg: err.Error(), Kind: packages.UnknownError})
	}
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
