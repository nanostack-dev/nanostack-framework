package genrepo

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strings"
)

// The fixes of the allowlist (E7).
const (
	rawFix = "build the expression with Nanostack DB constructors (columns, EQ, AND, postgres.LOWER, ...), " +
		"inside a function"
	followFix = "call a function of this package, or a Nanostack DB constructor, directly"
	sqlName   = `^[a-z_][a-z0-9_]*$`
	aliasSQL  = `^[a-z_][a-z0-9_]{0,62}$`
)

// sqlIdentifier matches the function name of Func and the type of CAST(x).AS.
var sqlIdentifier = regexp.MustCompile(sqlName) //nolint:gochecknoglobals // compiled once.

// sqlAlias matches an alias of AS: an SQL name that PostgreSQL keeps whole.
// PostgreSQL cuts a name to 63 bytes, so two longer aliases could be one
// name to it and another to the check.
var sqlAlias = regexp.MustCompile(aliasSQL) //nolint:gochecknoglobals // compiled once.

// refusedJoins are the joins that can bring rows that no tenant link
// reaches (E28).
var refusedJoins = []string{"RIGHT_JOIN", "FULL_JOIN", "CROSS_JOIN"} //nolint:gochecknoglobals // read-only.

// literalConstructors are the Nanostack DB functions that take a string or
// an untyped value and pass it as a bound value, never as SQL text.
var literalConstructors = []string{ //nolint:gochecknoglobals // read-only.
	"String", "Text", "Decimal", "StringArray", "Json", "Bytea", "Timestampz", "Timez",
}

// readWalk checks one From or expression function, and the functions of
// the package it calls, against the allowlist of design S5 (E7, E28). It
// collects the instance of each column and table it names, and the
// instances that each subquery reads.
type readWalk struct {
	c        *collector
	s        *searchDecl
	role     string // how a message names the function: "From itemsWith", "Flag Fresh fresh"
	followed map[*types.Func]bool
	reported map[string]bool // one message once, as a value used twice on a line
	uses     []instanceUse
	subs     []*subquery
	chains   map[token.Pos]token.Pos // the start of a SELECT chain -> its end
	wheres   map[token.Pos]ast.Expr  // the start of a SELECT chain -> its WHERE condition
}

// instanceUse is one place where a function names a table instance.
type instanceUse struct {
	table *tableBase
	alias string
	text  string // the table variable as written
	node  ast.Node
}

// subquery is a SELECT of a function: the instances that its FROM reads
// are known within the whole SELECT ... FROM ... WHERE chain, which starts
// at pos.
type subquery struct {
	pos   token.Pos
	reads []*instance
}

// walkFunc checks the function decl. role names it in messages.
func (c *collector) walkFunc(s *searchDecl, role string, decl *ast.FuncDecl) *readWalk {
	w := &readWalk{c: c, s: s, role: role, followed: map[*types.Func]bool{}, reported: map[string]bool{},
		chains: make(map[token.Pos]token.Pos), wheres: make(map[token.Pos]ast.Expr)}
	if fn, ok := c.l.pkg.TypesInfo.Defs[decl.Name].(*types.Func); ok {
		w.followed[fn] = true
	}
	w.body(decl)
	return w
}

// inner returns the instance of a subquery that u names, or nil.
func (w *readWalk) inner(u instanceUse) *instance {
	for _, sub := range w.subs {
		if u.node.Pos() < sub.pos || u.node.Pos() >= w.chains[sub.pos] {
			continue
		}
		for _, inst := range sub.reads {
			if inst.is(u.table, u.alias) {
				return inst
			}
		}
	}
	return nil
}

func (w *readWalk) body(decl *ast.FuncDecl) {
	if decl.Body != nil {
		ast.Inspect(decl.Body, w.visit)
	}
}

func (w *readWalk) walk(nodes ...ast.Expr) {
	for _, n := range nodes {
		ast.Inspect(n, w.visit)
	}
}

func (w *readWalk) visit(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		w.call(x)
		return false
	case *ast.SelectorExpr:
		return w.selector(x)
	case *ast.Ident:
		w.ident(x)
	case *ast.FuncLit:
		w.refuse(x, followFix, "declares a function literal, which nanostack gen repo cannot follow")
		return false
	}
	return true
}

// refuse reports one E7 problem at n, once per line and message.
func (w *readWalk) refuse(n ast.Node, fix, format string, args ...any) {
	msg := fmt.Sprintf("%s "+format, append([]any{w.role}, args...)...)
	file, line := w.c.l.position(w.c.l.pkg.Fset, n.Pos())
	if key := fmt.Sprint(file, line, msg); !w.reported[key] {
		w.reported[key] = true
		w.c.errorf(n, fix, "%s", msg)
	}
}

// call checks one call: a conversion, a builtin, a function or a method.
func (w *readWalk) call(call *ast.CallExpr) {
	info := w.c.l.pkg.TypesInfo
	if tv := info.Types[call.Fun]; tv.IsType() {
		w.conversion(call, tv.Type)
		w.walk(call.Args...)
		return
	}
	fun := ast.Unparen(call.Fun)
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		fun = ix.X
	case *ast.IndexListExpr:
		fun = ix.X
	}
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		if sel := info.Selections[f]; sel != nil {
			w.method(call, f, sel)
			return
		}
		w.function(call, info.Uses[f.Sel])
	case *ast.Ident:
		w.function(call, info.Uses[f])
	default:
		w.refuse(call, followFix, "calls %s, which nanostack gen repo cannot resolve", w.c.source(call.Fun))
	}
	w.walk(call.Args...)
}

// conversion refuses a conversion to a Nanostack DB or table type that is
// not an interface: postgres.Token("...") writes its text into the SQL.
func (w *readWalk) conversion(call *ast.CallExpr, to types.Type) {
	if _, isInterface := to.Underlying().(*types.Interface); isInterface || !w.c.dbTyped(to) {
		return
	}
	w.refuse(call, rawFix, "converts to %s: it writes SQL that the tenant check cannot read",
		w.c.source(call.Fun))
}

// function checks a call of a package-level function or function variable.
func (w *readWalk) function(call *ast.CallExpr, obj types.Object) {
	if _, isBuiltin := obj.(*types.Builtin); isBuiltin {
		return
	}
	if obj == nil || obj.Pkg() == nil || obj.Parent() != obj.Pkg().Scope() {
		w.refuse(call, followFix, "calls %s, which nanostack gen repo cannot resolve", w.c.source(call.Fun))
		return
	}
	sig, _ := obj.Type().Underlying().(*types.Signature)
	fn, isFunc := obj.(*types.Func)
	switch {
	case obj.Pkg() == w.c.l.pkg.Types && isFunc:
		w.follow(call, fn)
	case inDB(obj.Pkg()) && sig != nil:
		w.dbFunction(call, obj.Name(), sig)
	default:
		w.refuse(call, followFix, "calls %s, which nanostack gen repo cannot follow", w.c.source(call.Fun))
	}
}

// follow walks a function of the package that the function calls.
func (w *readWalk) follow(call *ast.CallExpr, fn *types.Func) {
	if w.followed[fn] {
		return
	}
	w.followed[fn] = true
	decl := w.c.funcDecl(fn)
	if decl == nil {
		w.refuse(call, followFix, "calls %s, which nanostack gen repo cannot follow", fn.Name())
		return
	}
	w.body(decl)
}

// dbFunction checks a call of a Nanostack DB function: the constructors
// that write SQL text are refused, Func and CAST take constant names, and
// the others must return an expression and take no SQL text.
func (w *readWalk) dbFunction(call *ast.CallExpr, name string, sig *types.Signature) {
	callee := w.c.source(call.Fun)
	switch {
	case name == "CustomExpression" || name == "NewEnumValue" || strings.HasPrefix(name, "Raw"):
		w.refuse(call, rawFix, "calls %s: it writes SQL that the tenant check cannot read", callee)
	case name == "Func":
		w.constantName(call, 0, `pass the function name as a string constant, such as postgres.Func("lower", x)`)
	case name == "CAST" || name == "SELECT":
	case name == "DATE_TRUNC":
		for i := 2; i < len(call.Args); i++ {
			if tv := w.c.l.pkg.TypesInfo.Types[call.Args[i]]; tv.Value == nil {
				w.refuse(call, `pass the time zone as a string constant, such as "UTC"`,
					"calls %s with the time zone %s, which is not a constant", callee, w.c.source(call.Args[i]))
			}
		}
	case takesText(sig) && !slices.Contains(literalConstructors, name):
		w.refuse(call, rawFix, "calls %s: it takes a string that becomes SQL text", callee)
	case w.c.statement(sig.Results()):
		w.refuse(call, "write the subquery as postgres.SELECT(...).FROM(table).WHERE(...), or the search by hand on "+
			"db/searchexec", "calls %s, which builds a statement: a subquery of a search reads with SELECT, FROM "+
			"and WHERE only", callee)
	case !w.c.expressions(sig.Results()):
		w.refuse(call, rawFix+"; write the search by hand on db/searchexec for other SQL",
			"calls %s, which does not build an expression", callee)
	}
}

// constantName checks that argument i of call is a string constant that is
// an SQL identifier.
func (w *readWalk) constantName(call *ast.CallExpr, i int, fix string) {
	if i < len(call.Args) {
		tv := w.c.l.pkg.TypesInfo.Types[call.Args[i]]
		if tv.Value != nil && tv.Value.Kind() == constant.String &&
			sqlIdentifier.MatchString(constant.StringVal(tv.Value)) {
			return
		}
	}
	w.refuse(call, fix, "calls %s with %s, which is not a constant that matches %s: it writes SQL text",
		w.c.source(call.Fun), argText(w.c, call, i), sqlName)
}

// method checks a method call.
func (w *readWalk) method(call *ast.CallExpr, f *ast.SelectorExpr, sel *types.Selection) {
	fn, _ := sel.Obj().(*types.Func)
	switch {
	case fn == nil:
		w.refuse(call, followFix, "calls %s, which nanostack gen repo cannot resolve", w.c.source(call.Fun))
	case w.c.isTableType(sel.Recv()) && !inDB(fn.Pkg()):
		w.tableMethod(call, f, fn.Name())
		return
	case inDB(fn.Pkg()):
		if !w.dbMethod(call, f, fn, sel.Recv()) {
			return
		}
	default:
		w.refuse(call, followFix, "calls the method %s, which nanostack gen repo cannot follow", w.c.source(call.Fun))
	}
	w.walk(f.X)
	w.walk(call.Args...)
}

// tableMethod checks a method of a generated table type: only AS, with a
// constant alias.
func (w *readWalk) tableMethod(call *ast.CallExpr, f *ast.SelectorExpr, name string) {
	if name != "AS" {
		w.refuse(call, "read the table as nanostack gen db generated it: "+w.c.source(f.X),
			"calls %s: it reads another table than the one whose facts nanostack gen db wrote", w.c.source(call.Fun))
		w.walk(f.X)
		return
	}
	if alias, ok := w.c.aliasName(w.s, call.Args[0], f.X); ok {
		w.use(f.X, alias, call)
	}
}

// dbMethod checks a method of a Nanostack DB type. It returns false when
// the walk stops there: a subquery clause outside the allowlist.
func (w *readWalk) dbMethod(call *ast.CallExpr, f *ast.SelectorExpr, fn *types.Func, recv types.Type) bool {
	name, sig := fn.Name(), fn.Signature()
	switch {
	case slices.Contains(refusedJoins, name):
		w.refuseJoin(call, f, name)
	case name == "INNER_JOIN" || name == "LEFT_JOIN":
	case w.c.isTableType(recv) || isPostgres(recv, "ReadableTable"):
		w.refuse(call, "write a subquery as postgres.SELECT(...).FROM(table).WHERE(...)",
			"calls %s: a table of a search is read with INNER_JOIN, LEFT_JOIN or a subquery's FROM",
			w.c.source(call.Fun))
		return false
	case isPostgres(recv, "cast"):
		if name == "AS" {
			w.constantName(call, 0, `pass the type as a string constant, such as CAST(x).AS("text"), `+
				"or use a typed cast, such as AS_TEXT()")
		}
	case isPostgres(recv, "SelectStatement"):
		w.chains[call.Pos()] = max(w.chains[call.Pos()], call.End())
		return w.selectClause(call, name)
	case takesText(sig):
		w.refuse(call, rawFix, "calls %s: it takes a string that becomes SQL text", w.c.source(call.Fun))
	case !w.c.expressions(sig.Results()):
		w.refuse(call, rawFix+"; write the search by hand on db/searchexec for other SQL",
			"calls %s, which does not build an expression", w.c.source(call.Fun))
	}
	return true
}

// selectClause allows SELECT ... FROM ... WHERE in a subquery, and records
// the instances that FROM reads.
func (w *readWalk) selectClause(call *ast.CallExpr, name string) bool {
	switch name {
	case "WHERE":
		if len(call.Args) == 1 {
			w.wheres[call.Pos()] = call.Args[0]
		}
	case "FROM":
		w.subqueryFrom(call)
	default:
		w.refuse(call, "write the subquery with SELECT, FROM and WHERE only, or the search by hand on "+
			"db/searchexec", "uses %s in a subquery: a subquery of a search reads with SELECT, FROM and WHERE only",
			name)
		return false
	}
	return true
}

// refuseJoin reports a RIGHT, FULL or CROSS join (E28), with the INNER JOIN
// on a declared foreign key as the fix when one links the table to the
// table of the search.
func (w *readWalk) refuseJoin(call *ast.CallExpr, f *ast.SelectorExpr, name string) {
	fix := "join with INNER_JOIN or LEFT_JOIN on the columns of a declared foreign key"
	if len(call.Args) > 0 {
		if joined := w.c.quietTable(w.s, call.Args[0]); joined != nil {
			text := w.c.source(tableOperand(call.Args[0]))
			if on, ok := fkCondition(joined, text, w.s.root.table, w.s.tableText); ok {
				fix = "INNER_JOIN(" + w.c.source(call.Args[0]) + ", " + on + ")"
			}
		}
	}
	w.c.errorf(call, fix, "%s uses %s: it brings rows that no tenant link reaches", w.role, name)
	w.walk(f.X)
}

// selector checks a selector that is not called: a column of an instance, a
// field of the scope value, or a qualified name.
func (w *readWalk) selector(x *ast.SelectorExpr) bool {
	info := w.c.l.pkg.TypesInfo
	if _, isPkg := info.Uses[identOf(x.X)].(*types.PkgName); isPkg {
		w.qualified(x, info.Uses[x.Sel])
		return false
	}
	sel := info.Selections[x]
	if sel == nil {
		return true
	}
	if sel.Kind() != types.FieldVal {
		w.refuse(x, followFix, "uses the method value %s, which nanostack gen repo cannot follow", w.c.source(x))
		return false
	}
	base := ast.Unparen(x.X)
	if v, isVar := info.Uses[identOf(base)].(*types.Var); isVar && v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
		w.packageVar(base, v)
		return false
	}
	if as, isAS := w.c.aliasCall(base); isAS {
		w.call(as)
		return false
	}
	if w.c.tableVariable(info.Uses[tableIdent(base)]) {
		w.use(base, "", x)
		return false
	}
	if w.c.isTableType(info.TypeOf(base)) {
		w.refuse(x, w.c.columnFix(base, x.Sel.Name),
			"reads %s through %s: nanostack gen repo reads the columns of table variables and of AS", x.Sel.Name,
			w.c.source(base))
		return false
	}
	if w.c.carriesSQL(sel.Type()) {
		w.refuse(x, "build the expression in the function from table columns and plain values",
			"reads %s, a field that can hold a prebuilt Nanostack DB value", w.c.source(x))
		return false
	}
	return true
}

// qualified checks a qualified name that is not called, such as
// table.Items or postgres.STAR.
func (w *readWalk) qualified(x *ast.SelectorExpr, obj types.Object) {
	switch v := obj.(type) {
	case *types.Var:
		w.packageVar(x, v)
	case *types.Func:
		w.refuse(x, followFix, "passes %s as a value, which nanostack gen repo cannot follow", w.c.source(x))
	}
}

// ident checks an identifier that is not called and not a selector.
func (w *readWalk) ident(id *ast.Ident) {
	switch v := w.c.l.pkg.TypesInfo.Uses[id].(type) {
	case *types.Var:
		if v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			w.packageVar(id, v)
		}
	case *types.Func:
		w.refuse(id, followFix, "passes %s as a value, which nanostack gen repo cannot follow", id.Name)
	}
}

// packageVar checks a package-level variable: a table variable of a
// generated table package is an instance; a variable of the dialect is
// one of its keywords, such as STAR; another variable of a Nanostack DB or
// table type hides what it reads.
func (w *readWalk) packageVar(n ast.Expr, v *types.Var) {
	_, isFunc := v.Type().Underlying().(*types.Signature)
	switch {
	case w.c.tableVariable(v):
		w.use(n, "", n)
	case inDB(v.Pkg()) && isFunc:
		w.refuse(n, followFix, "passes %s as a value, which nanostack gen repo cannot follow", w.c.source(n))
	case inDB(v.Pkg()):
	case w.c.carriesSQL(v.Type()):
		fix := "write the value in the function, so that nanostack gen repo follows it"
		if value := w.c.varValue(v); value != nil {
			fix = "write " + w.c.source(value) + " in the function in place of " + v.Name()
		}
		w.refuse(n, fix, "reads the package variable %s: nanostack gen repo reads functions, not prebuilt values",
			w.c.source(n))
	}
}

// use records the instance of a table expression: a table variable, with
// alias. Another expression of a table type is refused: nanostack gen repo would
// not know the table it reads.
func (w *readWalk) use(table ast.Expr, alias string, at ast.Node) {
	before := len(w.c.problems)
	t := w.c.quietTable(w.s, table)
	if t == nil {
		if w.walk(table); len(w.c.problems) == before {
			w.refuse(at, "write the table variable of the generated table package, such as table.Items, in place",
				"reads %s, which is not a table variable that nanostack gen db generated", w.c.source(table))
		}
		return
	}
	w.uses = append(w.uses, instanceUse{table: t, alias: alias, text: w.c.source(tableOperand(table)), node: at})
}

// subqueryFrom records the instances that the FROM of a subquery reads:
// the tables of its join, each with its alias.
func (w *readWalk) subqueryFrom(call *ast.CallExpr) {
	if len(call.Args) != 1 {
		w.refuse(call, "pass one table to FROM, joined with INNER_JOIN or LEFT_JOIN",
			"calls %s with %d tables", w.c.source(call.Fun), len(call.Args))
		return
	}
	sub := &subquery{pos: call.Pos()}
	for _, op := range joinOperands(call.Args[0]) {
		operand, alias := op.table, ""
		if as, isAS := w.c.aliasCall(operand); isAS {
			tv := w.c.l.pkg.TypesInfo.Types[as.Args[0]]
			if tv.Value == nil || tv.Value.Kind() != constant.String {
				continue // the walk reports it
			}
			alias = constant.StringVal(tv.Value)
		}
		t := w.c.quietTable(w.s, operand)
		if t == nil {
			w.refuse(operand, "pass a table variable of the generated table package, or its AS, to FROM, "+
				"such as table.Items or table.Items.AS(\"i\")",
				"reads %s in the FROM of a subquery, which is not a table variable: the check cannot know the "+
					"table it reads", w.c.source(operand))
			continue
		}
		text := w.c.source(tableOperand(operand))
		sub.reads = append(sub.reads, &instance{table: t, alias: alias, text: text, node: operand, join: op.kind,
			on: op.on, nullable: op.kind == "LEFT JOIN"})
	}
	w.subs = append(w.subs, sub)
}
