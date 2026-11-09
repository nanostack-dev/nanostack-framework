package genrepo

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// Import paths the generator recognises.
const (
	nanorepoPath   = "github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	dbPath         = "github.com/nanostack-dev/nanostack-framework/db"
	postgresPath   = dbPath + "/postgres"
	qrmPath        = dbPath + "/qrm"
	searchexecPath = dbPath + "/searchexec"
	functionalPath = "github.com/nanostack-dev/nanostack-framework/pkg/functional"
	transactorPath = "github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	uuidPath       = "github.com/google/uuid"
)

// dialects are the Nanostack DB dialects whose tables a declaration may
// name. Only postgres has a base in part 1 (design D20).
var dialects = map[string]string{ //nolint:gochecknoglobals // read-only table.
	dbPath + "/postgres": "PostgreSQL",
	dbPath + "/mysql":    "MySQL",
	dbPath + "/sqlite":   "SQLite",
}

// declarations are the declarations of one package and the tables they
// name.
type declarations struct {
	tables   []*tableBase   // in source order of their first declaration
	bindings []*binding     // nanorepo.Implement calls
	byImpl   map[string]int // implementation type name -> index in bindings
	searches []*searchDecl  // nanorepo.Search calls, in source order
}

// binding is one nanorepo.Implement[I, Impl](table, toDomain).
type binding struct {
	iface *types.Named
	impl  *types.Named
	table *tableBase
	file  string
	line  int
}

// at is the position of a declaration, relative to the package.
type at struct {
	file string
	line int
}

func (a at) String() string { return a.file + ":" + strconv.Itoa(a.line) }

// tableAtLine returns the table whose base holds line of the rendered src.
func (d *declarations) tableAtLine(src []byte, line int) *tableBase {
	lines := strings.Split(string(src), "\n")
	t := d.tables[0]
	for i := 0; i < len(lines) && i < line; i++ {
		for _, cand := range d.tables {
			if strings.HasPrefix(lines[i], "type "+cand.baseName()+" struct") {
				t = cand
			}
		}
	}
	return t
}

// declarations finds every declaration of the package and checks it. A
// package without one is an error: nano gen repo runs on declaring
// packages only.
func (l *loaded) declarations() (*declarations, error) {
	var problems []error
	for _, e := range l.pkg.Errors {
		if strings.HasPrefix(e.Msg, "could not import ") {
			file, line := gopkg.SplitPos(e.Pos)
			problems = append(problems, gen.Errorf(l.relPath(file), line, compileFix, "compile error: %s", e.Msg))
		}
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	d := &declarations{byImpl: map[string]int{}}
	c := &collector{l: l, d: d, searchNames: map[string]at{}, searchTables: map[*types.Var]*tableBase{},
		badAliases: map[string]bool{}}
	for _, f := range l.pkg.Syntax {
		if own(f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				c.call(call)
			}
			return true
		})
	}
	c.checkUnkeyedScopes()
	if len(c.problems) == 0 && len(d.tables) == 0 && len(d.searches) > 0 {
		c.problems = append(c.problems, l.searchWithoutRepository(d.searches[0]))
	}
	if len(c.problems) == 0 && len(d.tables) == 0 {
		c.problems = append(c.problems, gen.Errorf("", 0,
			"declare a repository with nanorepo.Implement or nanorepo.Base inside func _() { ... }, "+
				"in a file without a build tag",
			"package %s has no nanorepo declaration", l.pkg.Name))
	}
	if len(c.problems) == 0 {
		c.problems = append(c.problems, l.checkNames(d)...)
		c.problems = append(c.problems, l.checkBuilderNames(d)...)
	}
	if len(c.problems) > 0 {
		return nil, errors.Join(c.problems...)
	}
	return d, nil
}

// collector reads the declaration calls of one package.
type collector struct {
	l        *loaded
	d        *declarations
	problems []error

	searchNames  map[string]at             // search name -> its declaration (E11)
	searchTables map[*types.Var]*tableBase // the tables searches read; nil after a problem
	badAliases   map[string]bool           // file:line:alias of each alias reported, once per line
}

func (c *collector) errorf(n ast.Node, fix, format string, args ...any) {
	file, line := c.l.position(c.l.pkg.Fset, n.Pos())
	c.problems = append(c.problems, gen.Errorf(file, line, fix, format, args...))
}

// call handles one call expression: a declaration when it calls
// nanorepo.Implement or nanorepo.Base.
func (c *collector) call(call *ast.CallExpr) {
	fn, sel := calledFunc(c.l.pkg.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != nanorepoPath {
		return
	}
	if fn.Name() != "Implement" && fn.Name() != "Base" && fn.Name() != "Search" {
		return
	}
	if errs := c.compileErrors(call); len(errs) > 0 {
		c.problems = append(c.problems, errs...)
		return
	}
	inst, ok := c.l.pkg.TypesInfo.Instances[sel.Sel]
	if fn.Name() == "Search" {
		c.searchCall(call, inst, ok)
		return
	}
	if fn.Name() == "Implement" {
		if !ok || inst.TypeArgs.Len() != 4 || len(call.Args) != 2 {
			c.errorf(call, "write nanorepo.Implement[YourRepository, yourRepositoryImpl](table.YourTable, rowToDomain)",
				"nanorepo.Implement does not type-check")
			return
		}
		c.implement(call, inst.TypeArgs)
		return
	}
	if !ok || inst.TypeArgs.Len() != 1 || len(call.Args) != 1 {
		c.errorf(call, "write nanorepo.Base[model.YourRow](table.YourTable)", "nanorepo.Base does not type-check")
		return
	}
	row := inst.TypeArgs.At(0)
	c.bind(call, call.Args[0], row, row, nil)
}

// calledFunc resolves pkg.F(...), pkg.F[T](...) and pkg.F[T, U](...).
func calledFunc(info *types.Info, call *ast.CallExpr) (*types.Func, *ast.SelectorExpr) {
	var sel *ast.SelectorExpr
	switch fun := call.Fun.(type) {
	case *ast.IndexExpr:
		sel, _ = fun.X.(*ast.SelectorExpr)
	case *ast.IndexListExpr:
		sel, _ = fun.X.(*ast.SelectorExpr)
	case *ast.SelectorExpr:
		sel = fun
	}
	if sel == nil {
		return nil, nil
	}
	fn, _ := info.Uses[sel.Sel].(*types.Func)
	return fn, sel
}

// implement checks nanorepo.Implement[I, Impl, E, D](table, toDomain).
func (c *collector) implement(call *ast.CallExpr, args *types.TypeList) {
	iface, ok := types.Unalias(args.At(0)).(*types.Named)
	if !ok || !types.IsInterface(iface) {
		c.errorf(call, "pass the repository interface as the first type argument",
			"the first type argument of nanorepo.Implement is %s, not a named interface", c.qualified(args.At(0)))
		return
	}
	impl, ok := types.Unalias(args.At(1)).(*types.Named)
	_, isStruct := args.At(1).Underlying().(*types.Struct)
	if !ok || !isStruct || impl.Obj().Pkg() != c.l.pkg.Types {
		c.errorf(call,
			"pass the implementation struct, declared in package "+c.l.pkg.Name+", as the second type argument",
			"the second type argument of nanorepo.Implement is %s, not a struct type of package %s",
			c.qualified(args.At(1)), c.l.pkg.Name)
		return
	}
	if impl.TypeArgs().Len() > 0 {
		c.errorf(call, "bind a non-generic implementation type; to share code between repositories, "+
			"embed a generic helper in it",
			"%s is generic: nano gen repo binds an implementation type without type parameters",
			c.qualified(impl))
		return
	}
	if i, dup := c.d.byImpl[impl.Obj().Name()]; dup {
		first := c.d.bindings[i]
		c.errorf(call, "keep one nanorepo.Implement per implementation type",
			"%s is already bound to %s at %s", impl.Obj().Name(), c.qualified(first.iface),
			at{first.file, first.line})
		return
	}
	mapper, ok := c.mapper(call.Args[1], args.At(2))
	if !ok {
		return
	}
	t := c.bind(call, call.Args[0], args.At(2), args.At(3), mapper)
	if t == nil {
		return
	}
	file, line := c.l.position(c.l.pkg.Fset, call.Pos())
	c.d.byImpl[impl.Obj().Name()] = len(c.d.bindings)
	c.d.bindings = append(c.d.bindings, &binding{iface: iface, impl: impl, table: t, file: file, line: line})
}

// qualified renders t for a message: the declaring package's own names
// unqualified, others by package name.
func (c *collector) qualified(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == c.l.pkg.Types {
			return ""
		}
		return p.Name()
	})
}

// mapperRef is the mapper of a base: the object the generated code calls.
type mapperRef struct {
	obj  types.Object // a *types.Func or a package-level *types.Var
	text string       // as written in the declaration, for messages
}

// mapper checks the toDomain argument of nanorepo.Implement (design D5). It
// returns a nil ref for NoMapper. ok is false after a problem.
func (c *collector) mapper(arg ast.Expr, row types.Type) (*mapperRef, bool) {
	info := c.l.pkg.TypesInfo
	if fn, _ := calledFuncOfIndex(info, arg); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == nanorepoPath &&
		fn.Name() == "NoMapper" {
		return nil, true
	}
	text := c.source(arg)
	var id *ast.Ident
	switch x := arg.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		if _, isPkg := info.Uses[identOf(x.X)].(*types.PkgName); isPkg {
			id = x.Sel
		}
	}
	if id == nil {
		c.errorf(arg, "declare var <name>ToDomain = "+text+" in package "+c.l.pkg.Name+" and pass its name, "+
			"or pass a function",
			"the mapper %s is an expression; nano gen repo calls the mapper by name from the base", text)
		return nil, false
	}
	switch obj := info.Uses[id].(type) {
	case *types.Func:
		if sig, _ := obj.Type().(*types.Signature); sig != nil && sig.Recv() != nil {
			break // a method expression; the type check rejects it as a mapper
		}
		return &mapperRef{obj: obj, text: text}, true
	case *types.Var:
		if obj.Pkg() != c.l.pkg.Types || obj.Parent() != c.l.pkg.Types.Scope() {
			c.errorf(arg, "pass a function, or declare the variable in package "+c.l.pkg.Name,
				"the mapper %s is a variable of package %s; nano gen repo checks the value of a variable "+
					"of the declaring package only", text, obj.Pkg().Name())
			return nil, false
		}
		if !c.mapperVar(arg, obj, row) {
			return nil, false
		}
		return &mapperRef{obj: obj, text: text}, true
	}
	c.errorf(arg, "pass a function, or a variable of package "+c.l.pkg.Name+" set to one",
		"the mapper %s is not a function or a variable", text)
	return nil, false
}

// calledFuncOfIndex resolves nanorepo.NoMapper[model.X], an instantiated
// function value.
func calledFuncOfIndex(info *types.Info, e ast.Expr) (*types.Func, *ast.SelectorExpr) {
	ix, ok := e.(*ast.IndexExpr)
	if !ok {
		return nil, nil
	}
	return calledFunc(info, &ast.CallExpr{Fun: ix})
}

func identOf(e ast.Expr) *ast.Ident {
	id, _ := e.(*ast.Ident)
	return id
}

// mapperVar checks the value of a package-level mapper variable: a
// function, a function literal, or a method value on a type without fields.
// A method value on a type with fields means the mapper has state, which
// the base would never set (design D5).
func (c *collector) mapperVar(arg ast.Expr, v *types.Var, row types.Type) bool {
	value := c.varValue(v)
	if value == nil {
		c.errorf(arg, "set "+v.Name()+" in its declaration, to a function or a method value on a type without fields",
			"the mapper variable %s has no value in its declaration", v.Name())
		return false
	}
	info := c.l.pkg.TypesInfo
	switch x := ast.Unparen(value).(type) {
	case *ast.FuncLit:
		return true
	case *ast.Ident:
		if _, isFunc := info.Uses[x].(*types.Func); isFunc {
			return true
		}
	case *ast.SelectorExpr:
		if s := info.Selections[x]; s != nil && s.Kind() == types.MethodVal {
			recv := types.Unalias(s.Recv())
			if p, isPtr := recv.(*types.Pointer); isPtr {
				recv = types.Unalias(p.Elem())
			}
			if st, isStruct := recv.Underlying().(*types.Struct); isStruct && st.NumFields() > 0 {
				c.errorf(arg, "pass a plain function, or nanorepo.NoMapper["+c.qualified(row)+
					"] and map in hand-written methods",
					"the mapper %s is the method %s of %s, a type with fields: a mapper with state needs "+
						"dependencies that the base does not have", v.Name(), x.Sel.Name, c.qualified(recv))
				return false
			}
			return true
		}
		if _, isFunc := info.Uses[x.Sel].(*types.Func); isFunc {
			return true // pkg.Func
		}
	}
	c.errorf(arg, "set "+v.Name()+" to a function, a function literal, or a method value on a type without fields",
		"the mapper variable %s is set to %s, which nano gen repo cannot check", v.Name(), c.source(value))
	return false
}

// varValue returns the expression a package-level variable is declared
// with, or nil.
func (c *collector) varValue(v *types.Var) ast.Expr {
	for _, f := range c.l.pkg.Syntax {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				if value := c.specValue(spec, v); value != nil {
					return value
				}
			}
		}
	}
	return nil
}

// specValue returns the value spec gives v, or nil.
func (c *collector) specValue(spec ast.Spec, v *types.Var) ast.Expr {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Values) != len(vs.Names) {
		return nil
	}
	for i, name := range vs.Names {
		if c.l.pkg.TypesInfo.Defs[name] == v {
			return vs.Values[i]
		}
	}
	return nil
}

// bind resolves the table of a declaration and the base it shares with the
// other declarations of that table (design D6). mapper is nil for NoMapper
// and Base.
func (c *collector) bind(call *ast.CallExpr, arg ast.Expr, row, domain types.Type, mapper *mapperRef) *tableBase {
	tbl, ok := c.table(arg)
	if !ok {
		return nil
	}
	file, line := c.l.position(c.l.pkg.Fset, call.Pos())
	for _, t := range c.d.tables {
		if t.varObj != tbl.varObj {
			continue
		}
		switch {
		case !sameMapper(t.mapper, mapper):
			c.errorf(call, "pass "+mapperText(t.mapper)+" here too: the declarations of one table in one package "+
				"share one base",
				"table %s already has a base with mapper %s at %s; this declaration passes %s",
				t.sqlName, mapperText(t.mapper), at{t.file, t.line}, mapperText(mapper))
			return nil
		case !types.Identical(t.row, row):
			c.errorf(call, "map from "+c.qualified(t.row)+", as the declaration at "+at{t.file, t.line}.String(),
				"table %s already has a base with rows of %s at %s; this declaration reads %s",
				t.sqlName, c.qualified(t.row), at{t.file, t.line}, c.qualified(row))
			return nil
		}
		return t
	}
	tbl.row, tbl.domain, tbl.mapper, tbl.file, tbl.line = row, domain, mapper, file, line
	if !c.columns(call, tbl) {
		return nil
	}
	c.d.tables = append(c.d.tables, tbl)
	return tbl
}

func sameMapper(a, b *mapperRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.obj == b.obj
}

func mapperText(m *mapperRef) string {
	if m == nil {
		return "no mapper (NoMapper or Base)"
	}
	return m.text
}

// source renders an expression as written.
func (c *collector) source(e ast.Expr) string {
	return types.ExprString(e)
}

// checkNames reports a hand-written package-level name that the base would
// declare, and two tables whose bases would share a name.
func (l *loaded) checkNames(d *declarations) []error {
	var problems []error
	byName := map[string]*tableBase{}
	scope := l.pkg.Types.Scope()
	for _, t := range d.tables {
		for _, name := range t.declaredNames() {
			if other, dup := byName[name]; dup {
				problems = append(problems, gen.Errorf(t.file, t.line,
					"declare one of the two tables in another package",
					"tables %s (%s) and %s (%s) both give the generated name %s",
					other.sqlName, other.pkgPath, t.sqlName, t.pkgPath, name))
				continue
			}
			byName[name] = t
			if obj := scope.Lookup(name); obj != nil && obj.Pos().IsValid() {
				file, line := l.position(l.pkg.Fset, obj.Pos())
				problems = append(problems, gen.Errorf(file, line, "rename it",
					"%s is declared here, and nano gen repo declares it in %s for table %s",
					name, FileName, t.sqlName))
			}
		}
	}
	return problems
}
