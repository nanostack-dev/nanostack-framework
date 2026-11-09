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
// The allowlist check refuses the others first (E28).
var joins = map[string]string{ //nolint:gochecknoglobals // read-only table.
	"INNER_JOIN": "INNER JOIN", "LEFT_JOIN": "LEFT JOIN",
}

// from reads the function of nanorepo.From: a function of the declaring
// package, func() T or func(P) T, whose body returns a table joined to other
// tables. readSet reads its instances after the allowlist check (design S5).
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
	if ret, isReturn := onlyStatement(decl.Body).(*ast.ReturnStmt); !isReturn || len(ret.Results) != 1 {
		c.errorf(arg, fromFix, "search %s: From %s is not one return statement: nano gen repo reads the tables "+
			"of the join from it", s.name, fn.Name())
		return
	}
	s.from = &fromFunc{name: fn.Name(), takesScope: sig.Params().Len() == 1, arg: arg, decl: decl,
		scopeVars: map[*types.Var]bool{}}
	if s.from.takesScope {
		s.from.param = sig.Params().At(0).Type()
		if c.carriesSQL(s.from.param) {
			c.errorf(arg, "take the scope value, a struct of plain values such as strings and UUIDs",
				"search %s: From %s takes %s, which can hold a Nanostack DB value: the caller could put any "+
					"condition in the join", s.name, fn.Name(), c.qualified(s.from.param))
			s.from = nil
			return
		}
		if names := decl.Type.Params.List[0].Names; len(names) == 1 {
			if v, isVar := info.Defs[names[0]].(*types.Var); isVar {
				s.from.scopeVars[v] = true
			}
		}
	}
}

// fromReads reads the instances of the From function into s.reads, left to
// right, and sets the root: the first instance of the table of the search
// without an alias.
func (c *collector) fromReads(s *searchDecl) bool {
	ret, _ := onlyStatement(s.from.decl.Body).(*ast.ReturnStmt)
	if !c.joinExpr(s, ret.Results[0], map[*types.Func]bool{}) {
		return false
	}
	for _, inst := range s.reads {
		if inst.is(s.root.table, "") {
			s.root = inst
			return true
		}
	}
	c.errorf(s.from.arg, "join "+s.root.table.sqlName+" in "+s.from.name+", or search the table it starts from",
		"search %s: From %s does not read %s, the table of the search, without an alias", s.name, s.from.name,
		s.root.table.sqlName)
	return false
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

// joinExpr reads a join expression into s.reads, left to right. A call of
// a function of the package with one return statement is followed.
func (c *collector) joinExpr(s *searchDecl, e ast.Expr, followed map[*types.Func]bool) bool {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok {
		if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel {
			if kind, isJoin := joins[sel.Sel.Name]; isJoin && c.dbMethod(sel) {
				return c.join(s, kind, sel.X, call, followed)
			}
		}
		if helper := c.packageFunc(call); helper != nil {
			return c.followJoin(s, call, helper, followed)
		}
	}
	inst := c.instanceRef(s, e)
	return inst != nil && c.addRead(s, inst)
}

// followJoin reads the join that a function of the package returns.
func (c *collector) followJoin(s *searchDecl, call *ast.CallExpr, fn *types.Func,
	followed map[*types.Func]bool,
) bool {
	decl := c.funcDecl(fn)
	ret, isReturn := (*ast.ReturnStmt)(nil), false
	if decl != nil {
		ret, isReturn = onlyStatement(decl.Body).(*ast.ReturnStmt)
	}
	if followed[fn] || !isReturn || len(ret.Results) != 1 {
		c.errorf(call, fromFix, "search %s: From %s calls %s, which is not one return statement: nano gen repo "+
			"reads the tables of the join from it", s.name, s.from.name, fn.Name())
		return false
	}
	followed[fn] = true
	// A parameter of the helper is the scope value only when the caller
	// passes the scope value itself, not a value it builds.
	params := decl.Type.Params.List
	i := 0
	for _, field := range params {
		for _, name := range field.Names {
			v, _ := c.l.pkg.TypesInfo.Defs[name].(*types.Var)
			if i < len(call.Args) && v != nil && c.isScopeVar(s, call.Args[i]) {
				s.from.scopeVars[v] = true
			}
			i++
		}
		if len(field.Names) == 0 {
			i++
		}
	}
	return c.joinExpr(s, ret.Results[0], followed)
}

// packageFunc returns the function of the declaring package, without a
// receiver, that call calls, or nil.
func (c *collector) packageFunc(call *ast.CallExpr) *types.Func {
	fun := ast.Unparen(call.Fun)
	if ix, isIndex := fun.(*ast.IndexExpr); isIndex {
		fun = ix.X
	}
	fn, _ := c.l.pkg.TypesInfo.Uses[identOf(fun)].(*types.Func)
	if fn == nil || fn.Pkg() != c.l.pkg.Types || fn.Signature().Recv() != nil {
		return nil
	}
	return fn
}

// join reads left KIND_JOIN(table, on) and marks the nullable side of a
// LEFT JOIN. The allowlist check refused RIGHT, FULL and CROSS joins (E28).
func (c *collector) join(s *searchDecl, kind string, left ast.Expr, call *ast.CallExpr,
	followed map[*types.Func]bool,
) bool {
	if !c.joinExpr(s, left, followed) || len(call.Args) != 2 {
		return false
	}
	inst := c.instanceRef(s, call.Args[0])
	if inst == nil {
		return false
	}
	inst.join, inst.on, inst.nullable = kind, call.Args[1], kind == "LEFT JOIN"
	return c.addRead(s, inst)
}

// addRead adds an instance to the read set. Two instances of one name, such
// as tenants twice, or tenants AS owner twice, would make SQL refuse the
// query, or bind a column to the wrong instance.
func (c *collector) addRead(s *searchDecl, inst *instance) bool {
	for _, read := range s.reads {
		if read.name() != inst.name() {
			continue
		}
		fix := `give each instance its own alias, such as ` + inst.text + `.AS("` + inst.name() + `2")`
		c.errorf(inst.node, fix, "search %s: From %s reads %s twice: two instances of one name in a join "+
			"make the query ambiguous", s.name, s.from.name, inst.name())
		return false
	}
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
	node, alias := e, ""
	if call, ok := e.(*ast.CallExpr); ok {
		sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "AS" || len(call.Args) != 1 {
			c.errorf(call, fromFix,
				"search %s: From %s calls %s; nano gen repo reads table variables, AS and the join methods",
				s.name, s.from.name, c.source(call.Fun))
			return nil
		}
		var isString bool
		if alias, isString = c.aliasName(s, call.Args[0], sel.X); !isString {
			return nil
		}
		e = ast.Unparen(sel.X)
	}
	if _, isVar := c.l.pkg.TypesInfo.Uses[tableIdent(e)].(*types.Var); !isVar {
		c.errorf(e, fromFix,
			"search %s: From %s reads %s; nano gen repo reads table variables, AS and the join methods",
			s.name, s.from.name, c.source(e))
		return nil
	}
	t := c.searchTable(s, e)
	if t == nil {
		return nil
	}
	return &instance{table: t, alias: alias, text: c.source(e), node: node}
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
