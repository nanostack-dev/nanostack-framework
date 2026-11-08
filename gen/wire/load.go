package wire

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// NanoPath is the import path of the runtime package the manifest uses.
const NanoPath = "github.com/nanostack-dev/nanostack-framework/core/nano"

const buildTag = "nanogen"

// provider is one entry the manifest declares: a constructor, a binding, a
// collection, a collection member or an invoke.
type provider struct {
	kind   providerKind
	ref    token.Pos // where the manifest lists it
	owned  bool      // declared in the manifest's own package
	order  int       // declaration order across the whole manifest
	fn     *types.Func
	targs  []types.Type // type arguments of a generic provider
	params []types.Type
	out    types.Type // provided type; the interface for a binding, []T for a collection
	errRet bool       // returns an error last
	clean  bool       // returns a cleanup func()
	impl   types.Type // binding: the implementation
	elem   types.Type // collection and member: T of nano.Collect[T]
}

type providerKind int

const (
	kindFunc    providerKind = iota // nano.Providers
	kindBind                        // nano.Bind
	kindInvoke                      // nano.Invoke
	kindCollect                     // nano.Collect: the collection itself
	kindMember                      // nano.Collect: one member function
)

// manifest is everything the generator learned from the source.
type manifest struct {
	opts      Options
	fset      *token.FileSet
	root      *packages.Package
	nano      *types.Package
	file      string // manifest file, absolute
	appName   string
	items     []*provider
	config    *types.Named // the manifest package's Config type, if any
	outPath   string       // import path of the output package
	outName   string       // package name of the output package
	modDir    string
	modPath   string
	samePkg   bool // output goes into the manifest package
	ifaces    lifecycleIfaces
	appType   types.Type        // *nano.App
	fileToPkg map[string]string // loaded file -> import path
}

type lifecycleIfaces struct {
	starter, stopper, runner, checker *types.Interface
	appConfig                         types.Type
}

// loadManifest loads the manifest package and walks the manifest. Module
// variables declared in other packages need those packages' syntax, so the
// load repeats with them as extra roots until every module is readable; one
// load keeps one types universe, so type identity holds across packages.
func loadManifest(opts Options) (*manifest, error) {
	var extra []string
	for {
		pkgs, err := load(opts, extra)
		if err != nil {
			return nil, err
		}
		w := newWalker(opts, pkgs)
		m, err := w.walk()
		if err != nil {
			return nil, err
		}
		if len(w.needSyntax) == 0 {
			return m, nil
		}
		before := len(extra)
		for p := range w.needSyntax {
			if !slices.Contains(extra, p) {
				extra = append(extra, p)
			}
		}
		if len(extra) == before {
			return nil, fmt.Errorf("cannot load the syntax of %v", extra)
		}
		slices.Sort(extra)
	}
}

func load(opts Options, extra []string) ([]*packages.Package, error) {
	tags := append([]string{buildTag}, opts.Tags...)
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedModule,
		Dir:        opts.Dir,
		BuildFlags: []string{"-tags=" + strings.Join(tags, ",")},
	}
	pkgs, err := packages.Load(cfg, append([]string{"."}, extra...)...)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", opts.Dir, err)
	}
	var errs []error
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, errors.New(e.Error()))
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("the manifest package does not compile:\n%w", errors.Join(errs...))
	}
	return pkgs, nil
}

type walker struct {
	opts       Options
	m          *manifest
	pkgs       map[string]*packages.Package // loaded with syntax, by path
	needSyntax map[string]bool
	modules    map[*types.Var]bool // module variables on the current path
	d          diags
}

func newWalker(opts Options, pkgs []*packages.Package) *walker {
	w := &walker{
		opts:       opts,
		pkgs:       map[string]*packages.Package{},
		needSyntax: map[string]bool{},
		modules:    map[*types.Var]bool{},
	}
	for _, p := range pkgs {
		w.pkgs[p.PkgPath] = p
	}
	root := pkgs[0]
	for _, p := range pkgs {
		if len(p.GoFiles) > 0 && filepath.Dir(p.GoFiles[0]) == opts.Dir {
			root = p
		}
	}
	w.m = &manifest{opts: opts, fset: root.Fset, root: root, fileToPkg: map[string]string{}}
	for _, p := range pkgs {
		for _, f := range p.GoFiles {
			w.m.fileToPkg[f] = p.PkgPath
		}
	}
	return w
}

func (w *walker) pos(p token.Pos) string { return w.m.relPos(p) }

func (w *walker) walk() (*manifest, error) {
	m := w.m
	root := m.root
	if root.Module == nil {
		return nil, fmt.Errorf("%s is not in a Go module", w.opts.Dir)
	}
	m.modDir, m.modPath = root.Module.Dir, root.Module.Path

	for _, imp := range root.Types.Imports() {
		if imp.Path() == NanoPath {
			m.nano = imp
		}
	}
	spec, call := w.findManifest()
	if spec == nil {
		return nil, fmt.Errorf("no manifest in %s: declare var App = nano.Manifest(\"name\", ...) "+
			"in a file with the %s build tag", w.opts.Dir, buildTag)
	}
	if err := w.lookupNano(); err != nil {
		return nil, err
	}
	if err := w.resolveOutput(); err != nil {
		return nil, err
	}
	w.lookupConfig()

	m.file = m.fset.Position(spec.Pos()).Filename
	if len(call.Args) == 0 {
		w.d.add(w.pos(call.Pos()), "nano.Manifest needs the app name as its first argument")
		return nil, w.d.err()
	}
	if tv, ok := root.TypesInfo.Types[call.Args[0]]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		m.appName = constant.StringVal(tv.Value)
	} else {
		w.d.add(w.pos(call.Args[0].Pos()), "the app name passed to nano.Manifest must be a string constant")
	}
	for _, arg := range call.Args[1:] {
		w.item(root, arg)
	}
	if err := w.d.err(); err != nil {
		return nil, err
	}
	return m, nil
}

// findManifest returns the single package-level variable initialised with
// nano.Manifest(...).
func (w *walker) findManifest() (*ast.ValueSpec, *ast.CallExpr) {
	root := w.m.root
	var spec *ast.ValueSpec
	var call *ast.CallExpr
	for _, vs := range valueSpecs(root.Syntax) {
		for _, v := range vs.Values {
			c, ok := ast.Unparen(v).(*ast.CallExpr)
			if !ok || w.nanoCallee(root, c) != "Manifest" {
				continue
			}
			if spec != nil {
				w.d.add(
					w.pos(vs.Pos()),
					"second nano.Manifest in package %s (first at %s); a package holds one manifest",
					root.Name,
					w.pos(spec.Pos()),
				)
				continue
			}
			spec, call = vs, c
		}
	}
	return spec, call
}

func (w *walker) lookupNano() error {
	m := w.m
	if m.nano == nil {
		return fmt.Errorf("the manifest package does not import %s", NanoPath)
	}
	iface := func(name string) *types.Interface {
		obj := m.nano.Scope().Lookup(name)
		if obj == nil {
			return nil
		}
		it, _ := obj.Type().Underlying().(*types.Interface)
		return it
	}
	m.ifaces = lifecycleIfaces{
		starter: iface("Starter"),
		stopper: iface("Stopper"),
		runner:  iface("Runner"),
		checker: iface("HealthChecker"),
	}
	appObj := m.nano.Scope().Lookup("App")
	cfgObj := m.nano.Scope().Lookup("AppConfig")
	ifaces := m.ifaces
	if ifaces.starter == nil || ifaces.stopper == nil || ifaces.runner == nil || ifaces.checker == nil ||
		appObj == nil || cfgObj == nil {
		return fmt.Errorf("%s lacks the lifecycle API this generator targets; upgrade the framework",
			NanoPath)
	}
	m.appType = types.NewPointer(appObj.Type())
	m.ifaces.appConfig = cfgObj.Type()
	return nil
}

func (w *walker) resolveOutput() error {
	m := w.m
	out := filepath.Clean(w.opts.Out)
	if out == filepath.Clean(w.opts.Dir) {
		m.samePkg, m.outPath, m.outName = true, m.root.PkgPath, m.root.Name
		return nil
	}
	rel, err := filepath.Rel(m.modDir, out)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("output %s is outside module %s", out, m.modPath)
	}
	m.outPath = path.Join(m.modPath, filepath.ToSlash(rel))
	m.outName = sanitizeIdent(filepath.Base(out))
	if m.root.Name == "main" {
		return fmt.Errorf("the manifest package is main, which %s cannot import; "+
			"move the manifest to a library package or set -out to the manifest directory", m.outPath)
	}
	return nil
}

func (w *walker) lookupConfig() {
	obj, ok := w.m.root.Types.Scope().Lookup("Config").(*types.TypeName)
	if !ok {
		return
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return
	}
	if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
		w.d.add(w.pos(obj.Pos()), "Config must be a struct type; Build provides each of its fields")
		return
	}
	w.m.config = named
}
