package genrepo

import (
	"go/ast"
	"go/types"
	"strings"
)

// joinOperand is one table of a join expression, with its join kind and
// ON condition; both are empty for the first table.
type joinOperand struct {
	table ast.Expr
	kind  string
	on    ast.Expr
}

// joinOperands returns the tables of a join expression, left to right.
func joinOperands(e ast.Expr) []joinOperand {
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if isCall {
		if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel && len(call.Args) > 0 &&
			strings.HasSuffix(sel.Sel.Name, "_JOIN") {
			op := joinOperand{table: call.Args[0], kind: joins[sel.Sel.Name]}
			if len(call.Args) > 1 {
				op.on = call.Args[1]
			}
			return append(joinOperands(sel.X), op)
		}
	}
	return []joinOperand{{table: e}}
}

// tableOperand returns the table variable of X or X.AS(alias).
func tableOperand(e ast.Expr) ast.Expr {
	if call, ok := ast.Unparen(e).(*ast.CallExpr); ok {
		if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel && sel.Sel.Name == "AS" {
			return ast.Unparen(sel.X)
		}
	}
	return ast.Unparen(e)
}

// quietTable resolves a table variable expression, X or X.AS(alias), to
// the table a search reads; nil when it is not one.
func (c *collector) quietTable(s *searchDecl, e ast.Expr) *tableBase {
	e = tableOperand(e)
	v, _ := c.l.pkg.TypesInfo.Uses[tableIdent(e)].(*types.Var)
	if !c.tableVariable(v) {
		return nil
	}
	return c.searchTable(s, e)
}

// tableVariable reports whether obj is a table variable of a table package
// that nano gen db generated.
func (c *collector) tableVariable(obj types.Object) bool {
	v, isVar := obj.(*types.Var)
	if !isVar || v == nil || v.Pkg() == nil || v.Pkg() == c.l.pkg.Types || v.Parent() != v.Pkg().Scope() ||
		!c.isTableType(v.Type()) {
		return false
	}
	src, err := c.l.tableSource(v.Pkg().Path())
	if err != nil {
		return false
	}
	_, found := src.tables[v.Name()]
	return found
}

// columnFix is the fix of a column read through base, an expression of a
// table type that is not a table variable or its AS: the same column of the
// table variable that base is built from, or of the table variable of its
// type.
func (c *collector) columnFix(base ast.Expr, col string) string {
	const fix = "read the column of the table variable of the generated table package, such as "
	if call, isCall := ast.Unparen(base).(*ast.CallExpr); isCall {
		if f, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel &&
			c.tableVariable(c.l.pkg.TypesInfo.Uses[tableIdent(f.X)]) {
			return fix + c.source(f.X) + "." + col
		}
	}
	named := tableNamed(c.l.pkg.TypesInfo.TypeOf(base))
	scope := named.Obj().Pkg().Scope()
	for _, name := range scope.Names() {
		if v, isVar := scope.Lookup(name).(*types.Var); isVar && tableNamed(v.Type()) == named {
			return fix + v.Pkg().Name() + "." + name + "." + col
		}
	}
	return "read the column of a table variable of the generated table package, or of its AS"
}

// argText is argument i of call as written, for a message.
func argText(c *collector, call *ast.CallExpr, i int) string {
	if i < len(call.Args) {
		return c.source(call.Args[i])
	}
	return "no name"
}

// isTableType reports whether t is a generated table type, or a pointer to
// one: a struct that embeds the Table of a dialect.
func (c *collector) isTableType(t types.Type) bool {
	named := tableNamed(t)
	return named != nil && embeddedDialect(named, 0) != ""
}

// dbTyped reports whether t is, or is built from, a Nanostack DB type or a
// generated table type.
func (c *collector) dbTyped(t types.Type) bool {
	switch x := types.Unalias(t).(type) {
	case *types.Pointer:
		return c.dbTyped(x.Elem())
	case *types.Slice:
		return c.dbTyped(x.Elem())
	case *types.Array:
		return c.dbTyped(x.Elem())
	case *types.Map:
		return c.dbTyped(x.Key()) || c.dbTyped(x.Elem())
	case *types.Named:
		return inDB(x.Obj().Pkg()) || c.isTableType(x)
	}
	return false
}

// carriesSQL reports whether a value of type t can hold a Nanostack DB
// value, such as postgres.RawBool(...): a DB or table type, an interface, a
// function or a channel, or a type built from one of them. A struct is read
// field by field, so uuid.UUID (an array of bytes) carries no SQL.
func (c *collector) carriesSQL(t types.Type) bool {
	return c.carries(t, map[types.Type]bool{})
}

func (c *collector) carries(t types.Type, seen map[types.Type]bool) bool {
	t = types.Unalias(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	switch x := t.(type) {
	case *types.Named:
		return inDB(x.Obj().Pkg()) || c.isTableType(x) || c.carries(x.Underlying(), seen)
	case *types.Interface, *types.Signature, *types.Chan, *types.TypeParam:
		return true
	case *types.Pointer:
		return c.carries(x.Elem(), seen)
	case *types.Slice:
		return c.carries(x.Elem(), seen)
	case *types.Array:
		return c.carries(x.Elem(), seen)
	case *types.Map:
		return c.carries(x.Key(), seen) || c.carries(x.Elem(), seen)
	case *types.Struct:
		for f := range x.Fields() {
			if c.carries(f.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// expressions reports whether every result of a signature is a Nanostack
// DB expression.
func (c *collector) expressions(results *types.Tuple) bool {
	expr := c.postgresInterface("Expression")
	if expr == nil || results.Len() == 0 {
		return false
	}
	for r := range results.Variables() {
		t := r.Type()
		if !types.Implements(t, expr) && !types.Implements(types.NewPointer(t), expr) {
			return false
		}
	}
	return true
}

// statement reports whether a result of a signature is a statement, such
// as the SetStatement of UNION.
func (c *collector) statement(results *types.Tuple) bool {
	stmt := c.postgresInterface("Statement")
	for r := range results.Variables() {
		if stmt != nil && (types.Implements(r.Type(), stmt) || types.Implements(types.NewPointer(r.Type()), stmt)) {
			return true
		}
	}
	return false
}

// postgresInterface returns the interface name of the PostgreSQL dialect,
// from the packages the declaring package imports.
func (c *collector) postgresInterface(name string) *types.Interface {
	var found *types.Interface
	seen := map[*types.Package]bool{}
	var visit func(p *types.Package)
	visit = func(p *types.Package) {
		if found != nil || seen[p] {
			return
		}
		seen[p] = true
		if p.Path() == postgresPath {
			if obj := p.Scope().Lookup(name); obj != nil {
				found, _ = obj.Type().Underlying().(*types.Interface)
			}
			return
		}
		for _, imp := range p.Imports() {
			visit(imp)
		}
	}
	visit(c.l.pkg.Types)
	return found
}

// takesText reports whether a signature takes a string or an untyped
// value, which can carry SQL text.
func takesText(sig *types.Signature) bool {
	for p := range sig.Params().Variables() {
		t := p.Type()
		if s, isSlice := t.(*types.Slice); isSlice {
			t = s.Elem()
		}
		switch u := t.Underlying().(type) {
		case *types.Basic:
			if u.Info()&types.IsString != 0 {
				return true
			}
		case *types.Interface:
			if u.Empty() {
				return true
			}
		}
	}
	return false
}

// inDB reports whether p is a package of the Nanostack DB module.
func inDB(p *types.Package) bool {
	return p != nil && strings.HasPrefix(p.Path(), dbPath+"/")
}

// isPostgres reports whether t is, or points to, the named type name of the
// PostgreSQL dialect.
func isPostgres(t types.Type, name string) bool {
	if p, isPointer := types.Unalias(t).(*types.Pointer); isPointer {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == postgresPath && n.Obj().Name() == name
}
