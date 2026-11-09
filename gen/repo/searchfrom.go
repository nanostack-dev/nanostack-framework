package genrepo

import (
	"go/ast"
	"go/types"
	"strings"
)

// fromFix is the fix of a From function that nano gen repo cannot read.
const fromFix = "declare the function in this package with one statement, " +
	"return table.X.INNER_JOIN(table.Y, <condition>) (or LEFT_JOIN), and pass its name to nanorepo.From"

// joins maps the join methods of Nanostack DB tables to their SQL.
var joins = map[string]string{ //nolint:gochecknoglobals // read-only table.
	"INNER_JOIN": "INNER JOIN", "LEFT_JOIN": "LEFT JOIN", "RIGHT_JOIN": "RIGHT JOIN", "FULL_JOIN": "FULL JOIN",
	"CROSS_JOIN": "CROSS JOIN",
}

// from reads the function of nanorepo.From into the instances of the
// search (design S5): a function of the declaring package, func() T or
// func(P) T, whose body returns a table joined to other tables. The first
// table without an alias of the searched table is the root. G3 reads the
// join kinds and marks the nullable side of each LEFT JOIN; G5 adds the
// alias rules, the refusal of RIGHT, FULL and CROSS joins, and the allowlist.
func (c *collector) from(s *searchDecl, arg ast.Expr) {
	info := c.l.pkg.TypesInfo
	if _, isLit := ast.Unparen(arg).(*ast.FuncLit); isLit {
		c.errorf(arg, fromFix, "search %s: From takes a function literal; nano gen repo reads a function of "+
			"package %s by its name", s.name, c.l.pkg.Name)
		return
	}
	fn, _ := info.Uses[identOf(ast.Unparen(arg))].(*types.Func)
	if fn == nil || fn.Pkg() != c.l.pkg.Types || fn.Signature().Recv() != nil {
		c.errorf(arg, fromFix, "search %s: From(%s) is not a function of package %s", s.name, c.source(arg),
			c.l.pkg.Name)
		return
	}
	sig := fn.Signature()
	decl := c.funcDecl(fn)
	if decl == nil || sig.Params().Len() > 1 || sig.Results().Len() != 1 || sig.Variadic() {
		c.errorf(arg, fromFix, "search %s: From(%s) takes no parameter or the scope value, and returns the table",
			s.name, fn.Name())
		return
	}
	ret, isReturn := onlyStatement(decl.Body).(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		c.errorf(arg, fromFix, "search %s: From %s is not one return statement: nano gen repo reads the tables "+
			"of the join from it", s.name, fn.Name())
		return
	}
	s.from = &fromFunc{name: fn.Name(), takesScope: sig.Params().Len() == 1, arg: arg}
	if !c.joinExpr(s, ret.Results[0]) {
		return
	}
	for _, inst := range s.reads {
		if inst.table == s.root.table && inst.alias == "" {
			s.root = inst
			return
		}
	}
	c.errorf(arg, "join "+s.root.table.sqlName+" in "+fn.Name()+", or search the table it starts from",
		"search %s: From %s does not read %s, the table of the search, without an alias", s.name, fn.Name(),
		s.root.table.sqlName)
}

// onlyStatement returns the statement of a body with one statement, or nil.
func onlyStatement(body *ast.BlockStmt) ast.Stmt {
	if body == nil || len(body.List) != 1 {
		return nil
	}
	return body.List[0]
}

// funcDecl returns the declaration of a function of the package.
func (c *collector) funcDecl(fn *types.Func) *ast.FuncDecl {
	for _, f := range c.l.pkg.Syntax {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && c.l.pkg.TypesInfo.Defs[fd.Name] == fn {
				return fd
			}
		}
	}
	return nil
}

// joinExpr reads a join expression into s.reads, left to right.
func (c *collector) joinExpr(s *searchDecl, e ast.Expr) bool {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok {
		if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel {
			if kind, isJoin := joins[sel.Sel.Name]; isJoin && c.dbMethod(sel) {
				return c.join(s, kind, sel.X, call)
			}
		}
	}
	inst := c.instanceRef(s, e)
	if inst == nil {
		return false
	}
	s.reads = append(s.reads, inst)
	return true
}

// join reads left KIND_JOIN(table, on) and marks the nullable side of a
// LEFT JOIN.
func (c *collector) join(s *searchDecl, kind string, left ast.Expr, call *ast.CallExpr) bool {
	if !c.joinExpr(s, left) || len(call.Args) == 0 {
		return false
	}
	inst := c.instanceRef(s, call.Args[0])
	if inst == nil {
		return false
	}
	inst.join = kind
	if len(call.Args) > 1 {
		inst.on = call.Args[1]
	}
	// A RIGHT, FULL or CROSS join can bring rows that no tenant link reaches:
	// G5 refuses them (E28), so only a LEFT JOIN has a nullable side here.
	inst.nullable = kind == "LEFT JOIN"
	s.reads = append(s.reads, inst)
	return true
}

// dbMethod reports whether sel names a method of a Nanostack DB type.
func (c *collector) dbMethod(sel *ast.SelectorExpr) bool {
	fn, _ := c.l.pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
	return fn != nil && fn.Pkg() != nil && strings.HasPrefix(fn.Pkg().Path(), dbPath+"/")
}

// instanceRef reads one table of a join: a table variable, or a table
// variable with AS("alias").
func (c *collector) instanceRef(s *searchDecl, e ast.Expr) *instance {
	e = ast.Unparen(e)
	alias := ""
	if call, ok := e.(*ast.CallExpr); ok {
		sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "AS" || len(call.Args) != 1 {
			c.errorf(s.from.arg, fromFix,
				"search %s: From %s calls %s; nano gen repo reads table variables, AS and the join methods",
				s.name, s.from.name, c.source(call.Fun))
			return nil
		}
		var isString bool
		if alias, isString = c.constString(s, call.Args[0]); !isString {
			return nil
		}
		e = sel.X
	}
	if _, isVar := c.l.pkg.TypesInfo.Uses[tableIdent(e)].(*types.Var); !isVar {
		c.errorf(s.from.arg, fromFix,
			"search %s: From %s reads %s; nano gen repo reads table variables, AS and the join methods",
			s.name, s.from.name, c.source(e))
		return nil
	}
	t := c.searchTable(s, e)
	if t == nil {
		return nil
	}
	return &instance{table: t, alias: alias}
}

// tableIdent returns the identifier of a table variable expression: X or
// pkg.X.
func tableIdent(e ast.Expr) *ast.Ident {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	}
	return nil
}
