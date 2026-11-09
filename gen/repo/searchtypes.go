package genrepo

import (
	"go/ast"
	"go/types"
	"strings"
)

// joinOperands returns the tables of a join expression, left to right.
func joinOperands(e ast.Expr) []ast.Expr {
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if isCall {
		if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel && len(call.Args) > 0 &&
			strings.HasSuffix(sel.Sel.Name, "_JOIN") {
			return append(joinOperands(sel.X), call.Args[0])
		}
	}
	return []ast.Expr{e}
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
	if !isVar || v.Pkg() == nil || v.Pkg() == c.l.pkg.Types || v.Parent() != v.Pkg().Scope() ||
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

// expressions reports whether every result of a signature is a Nanostack
// DB expression.
func (c *collector) expressions(results *types.Tuple) bool {
	expr := c.expressionInterface()
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

// expressionInterface returns the Expression interface of the PostgreSQL
// dialect, from the packages the declaring package imports.
func (c *collector) expressionInterface() *types.Interface {
	var found *types.Interface
	seen := map[*types.Package]bool{}
	var visit func(p *types.Package)
	visit = func(p *types.Package) {
		if found != nil || seen[p] {
			return
		}
		seen[p] = true
		if p.Path() == postgresPath {
			if obj := p.Scope().Lookup("Expression"); obj != nil {
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
