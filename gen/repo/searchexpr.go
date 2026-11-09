package genrepo

import (
	"go/ast"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// exprKinds maps the expression type of a SortExpr function to the
// db/keyset constructor of its computed key and the Go type of its value.
var exprKinds = map[string]struct { //nolint:gochecknoglobals // a lookup table
	ctor, value string
}{
	"IntegerExpression":    {"IntExprKey", "int64"},
	"FloatExpression":      {"FloatExprKey", "float64"},
	"StringExpression":     {"StringExprKey", "string"},
	"TimestampzExpression": {"TimeExprKey", "time.Time"},
}

// exprKey works out the computed key of a SortExpr (design S9): the Row
// field that carries the value, of the expression's Go type, a pointer when
// the expression can be NULL (E15); and a subquery that only aggregates
// (E8). nil after a problem.
func (c *collector) exprKey(b *builder, sort *searchSort) *sortKey {
	s := b.s
	fn, _ := c.funcObj(s, "SortExpr", sort.expr).(*types.Func)
	if fn == nil {
		return nil
	}
	what := "SortExpr(" + strconv.Quote(sort.field) + ", " + fn.Name() + ")"
	sig := fn.Signature()
	kind := ""
	if sig.Params().Len() == 0 && sig.Results().Len() == 1 {
		kind = dbTypeName(sig.Results().At(0).Type())
	}
	ek, known := exprKinds[kind]
	if !known {
		c.errorf(sort.expr, "return a postgres.IntegerExpression, FloatExpression, StringExpression or "+
			"TimestampzExpression from "+fn.Name()+", with no parameter",
			"search %s: %s: a computed key orders on an integer, float, string or timestamptz expression", s.name,
			what)
		return nil
	}
	value := ek.value
	field, ptr := c.exprField(s, sort.field)
	switch {
	case field == nil:
		c.errorf(sort.expr, "add the field "+sort.field+" "+value+" to "+c.qualified(s.row),
			"search %s: %s: the Row %s has no exported field %s to carry the value", s.name, what,
			c.qualified(s.row), sort.field)
		return nil
	case types.TypeString(elemOf(field.Type()), nil) != value:
		c.errorf(sort.expr, "make "+sort.field+" "+value+", or *"+value+" when the expression can be NULL",
			"search %s: %s: %s.%s is %s, and the expression gives %s", s.name, what, c.qualified(s.row), sort.field,
			c.qualified(field.Type()), value)
		return nil
	}
	var ret ast.Expr
	if decl := c.funcDecl(fn); decl != nil {
		if r, ok := onlyStatement(decl.Body).(*ast.ReturnStmt); ok && len(r.Results) == 1 {
			ret = r.Results[0]
		}
	}
	item, sub := c.selected(ret)
	if sub && !c.aggregate(item) {
		c.errorf(sort.expr, "select an aggregate of the child rows, such as postgres.COUNT(...) or postgres.MAX(...)",
			"%s: the subquery of %s reads a column instead of an aggregate: another row's value would go into "+
				"the Row and the cursor", "SortExpr "+sort.field, fn.Name())
		return nil
	}
	if !ptr && !c.notNull(item) {
		c.errorf(sort.expr, "make "+sort.field+" *"+value+", or wrap the expression in "+
			"postgres.COALESCE(..., <literal>)",
			"search %s: %s: %s can be NULL (it is not a top-level COUNT or COALESCE), and %s.%s is %s", s.name,
			what, fn.Name(), c.qualified(s.row), sort.field, c.qualified(field.Type()))
		return nil
	}
	ctor := ek.ctor
	if ptr {
		ctor = "Nullable" + ctor
	}
	return &sortKey{ctor: ctor, field: sort.field, value: field.Type(), expr: fn,
		computed: snake(rowTypeName(s.row)) + "." + snake(sort.field), nulls: sort.nulls,
		varName: specVar(s) + sort.field + "Key"}
}

// exprField returns the exported field name of the Row, and whether it is
// a pointer.
func (c *collector) exprField(s *searchDecl, name string) (*types.Var, bool) {
	obj, _, _ := types.LookupFieldOrMethod(s.row, false, c.l.pkg.Types, name)
	v, ok := obj.(*types.Var)
	if !ok || !v.IsField() || !v.Exported() {
		return nil, false
	}
	_, ptr := types.Unalias(v.Type()).(*types.Pointer)
	return v, ptr
}

// elemOf returns the element of a pointer type, or t.
func elemOf(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// rowTypeName is the name of the Row type, as the scan reads it.
func rowTypeName(t types.Type) string {
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return n.Obj().Name()
	}
	return "row"
}

// selected unwraps an expression: the casts IntExp, FloatExp, StringExp
// and TimestampzExp, then a scalar subquery SELECT(item).FROM(...)...,
// whose item it returns with sub true.
func (c *collector) selected(e ast.Expr) (ast.Expr, bool) {
	for {
		call, ok := ast.Unparen(e).(*ast.CallExpr)
		if !ok {
			return e, false
		}
		switch name := c.dbFuncName(call); name {
		case "IntExp", "FloatExp", "StringExp", "TimestampzExp":
			if len(call.Args) == 1 {
				e = call.Args[0]
				continue
			}
		case "SELECT":
			if len(call.Args) == 1 {
				return call.Args[0], true
			}
			return nil, true
		}
		// a method of the statement: FROM, WHERE, GROUP_BY, LIMIT
		sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !isSel || !c.dbMethod(sel) || !slices.Contains([]string{"FROM", "WHERE", "GROUP_BY", "HAVING", "LIMIT"},
			sel.Sel.Name) {
			return e, false
		}
		inner, sub := c.selected(sel.X)
		if !sub {
			return e, false
		}
		return inner, true
	}
}

// aggregate reports whether e is COUNT, SUM, MIN, MAX or AVG, or COALESCE
// of one of them.
func (c *collector) aggregate(e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	switch c.dbFuncName(call) {
	case "COUNT", "SUM", "SUMi", "SUMf", "MIN", "MINi", "MINf", "MAX", "MAXi", "MAXf", "AVG":
		return true
	case "COALESCE":
		return len(call.Args) > 0 && c.aggregate(call.Args[0])
	}
	return false
}

// notNull reports whether e cannot be NULL by its syntax: a top-level
// COUNT, or COALESCE whose last argument is a literal constructor.
func (c *collector) notNull(e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	switch c.dbFuncName(call) {
	case "COUNT":
		return true
	case "COALESCE":
		last, isCall := ast.Unparen(call.Args[len(call.Args)-1]).(*ast.CallExpr)
		return isCall && slices.Contains([]string{"Int", "Int32", "Int64", "Float", "String", "Bool",
			"Timestampz"}, c.dbFuncName(last))
	}
	return false
}

// dbFuncName returns the name of a package-level function or function
// variable of the DB module that call calls, or "".
func (c *collector) dbFuncName(call *ast.CallExpr) string {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return ""
	}
	// db/postgres declares some functions as package variables:
	// var COUNT = jet.COUNT.
	switch obj := c.l.pkg.TypesInfo.Uses[id].(type) {
	case *types.Func:
		if obj.Signature().Recv() == nil && inDB(obj.Pkg()) {
			return obj.Name()
		}
	case *types.Var:
		if inDB(obj.Pkg()) && obj.Parent() == obj.Pkg().Scope() {
			return obj.Name()
		}
	}
	return ""
}

// existsLink sets the root column that the link column of a FilterExists
// references, from a declared foreign key of the child to the searched
// table (E8): the subquery correlates on it, so the child row is the
// searched row's own data.
func (c *collector) existsLink(s *searchDecl, f *searchFilter) bool {
	root := s.root.table
	for _, k := range f.child.foreignKeys {
		if len(k.columns) != 1 || k.columns[0] != f.link.col.sqlName || k.references != root.sqlName ||
			(k.refSchema != "" && k.refSchema != root.schema) {
			continue
		}
		for _, col := range root.columns {
			if col.sqlName == k.refColumns[0] {
				f.ref, f.key = &colRef{table: root, alias: s.root.alias, tableText: s.root.text, col: col}, k.name
				return true
			}
		}
	}
	var keys []string
	for _, k := range f.child.foreignKeys {
		if k.references == root.sqlName && len(k.columns) == 1 {
			keys = append(keys, f.child.goName+"."+columnField(f.child, k.columns[0])+" ("+k.name+")")
		}
	}
	fix := "declare a foreign key from " + f.child.sqlName + " to " + root.sqlName + " and link on its column"
	if len(keys) > 0 {
		fix = "link on the column of a foreign key to " + root.sqlName + ": " + strings.Join(keys, ", ")
	}
	c.errorf(f.link.expr, fix, "search %s: FilterExists(%s, %s, ...): %s.%s is not the column of a declared "+
		"foreign key to %s, so the subquery is not correlated on the searched row", s.name, f.child.sqlName,
		f.link, f.child.sqlName, f.link.col.sqlName, root.sqlName)
	return false
}

// columnField is the Go field of the column sqlName of t.
func columnField(t *tableBase, sqlName string) string {
	for _, col := range t.columns {
		if col.sqlName == sqlName {
			return col.field
		}
	}
	return sqlName
}
