package genrepo

import (
	"go/ast"
	"go/constant"
	"go/types"
	"slices"
	"strings"
)

// readSet reads the instances of a search (design S5) after its options:
// it checks the From and expression functions against the allowlist (E7,
// E28), reads the join of From, checks that each join adds at most one row
// (E31), and that the functions read only instances the search reads (E6).
func (c *collector) readSet(s *searchDecl) {
	before := len(c.problems)
	var fromWalk *readWalk
	if s.from != nil {
		fromWalk = c.walkFunc(s, "From "+s.from.name, s.from.decl)
	}
	walks := c.walkExprs(s)
	if len(c.problems) > before {
		return
	}
	if s.from != nil {
		if !c.fromReads(s) {
			return
		}
		if c.checkUses(s, fromWalk); len(c.problems) > before {
			return
		}
		c.oneRowJoins(s)
	}
	for _, w := range walks {
		c.checkUses(s, w)
	}
}

// walkExprs checks the function of each expression option: a function of
// the package, named.
func (c *collector) walkExprs(s *searchDecl) []*readWalk {
	var walks []*readWalk
	for _, e := range s.exprs {
		fn, _ := c.l.pkg.TypesInfo.Uses[identOf(ast.Unparen(e.fn))].(*types.Func)
		var decl *ast.FuncDecl
		if fn != nil && fn.Pkg() == c.l.pkg.Types && fn.Signature().Recv() == nil {
			decl = c.funcDecl(fn)
		}
		if decl == nil {
			what := c.source(e.fn)
			if _, isLit := ast.Unparen(e.fn).(*ast.FuncLit); isLit {
				what = "a function literal"
			}
			c.errorf(e.fn, "declare the function in this package, and pass its name",
				"search %s: %s takes %s; nano gen repo reads a function of package %s by its name", s.name, e.role,
				what, c.l.pkg.Name)
			continue
		}
		walks = append(walks, c.walkFunc(s, e.role+" "+fn.Name(), decl))
	}
	return walks
}

// checkUses reports each instance that a function names and the search
// does not read (E6). An instance of a subquery's FROM is read within that
// subquery.
func (c *collector) checkUses(s *searchDecl, w *readWalk) {
	for _, sub := range w.subs {
		for i, inst := range sub.reads {
			in := innerRead{role: w.role, inst: inst}
			if i == 0 {
				in.where = w.wheres[sub.pos]
			}
			s.inner = append(s.inner, in)
		}
	}
	reported := map[string]bool{}
	for _, u := range w.uses {
		if w.inner(u) != nil {
			continue
		}
		if at := slices.IndexFunc(s.reads, func(i *instance) bool { return i.is(u.table, u.alias) }); at >= 0 {
			c.laterInstance(s, w, u, at)
			continue
		}
		inst := &instance{table: u.table, alias: u.alias, text: u.text}
		if reported[inst.String()] {
			continue
		}
		reported[inst.String()] = true
		c.errorf(u.node, c.joinFix(s, inst), "%s reads %s, which search %s does not read", w.role, inst, s.name)
	}
}

// laterInstance reports a use in the ON condition of a join that reads an
// instance that is joined after it: SQL refuses the query at run time.
// at is the position of the instance that u reads in s.reads.
func (c *collector) laterInstance(s *searchDecl, w *readWalk, u instanceUse, at int) {
	if s.from == nil || !strings.HasPrefix(w.role, "From ") {
		return
	}
	for i, inst := range s.reads {
		if inst.on == nil || u.node.Pos() < inst.on.Pos() || u.node.Pos() >= inst.on.End() || at <= i {
			continue
		}
		c.errorf(u.node, "join "+s.reads[at].String()+" before "+inst.String()+", or read it in the ON of a later join",
			"%s reads %s in the ON of %s, which is joined after it", w.role, s.reads[at], inst)
		return
	}
}

// joinFix is the fix of a column or function that reads an instance the
// search does not read: the join on a declared foreign key, when one links
// the table to the table of the search.
func (c *collector) joinFix(s *searchDecl, inst *instance) string {
	text := inst.text
	if inst.alias != "" {
		text += `.AS("` + inst.alias + `")`
	}
	on, ok := fkCondition(inst.table, text, s.root.table, s.tableText)
	switch {
	case ok && s.from != nil:
		return "join it in " + s.from.name + ": INNER_JOIN(" + text + ", " + on + ")"
	case ok:
		return "read it with nanorepo.From(fn), where fn returns " + s.tableText + ".INNER_JOIN(" + text + ", " +
			on + ")"
	}
	names := make([]string, len(s.reads))
	for i, read := range s.reads {
		names[i] = read.String()
	}
	return "join " + inst.String() + " in the From function, or read only " + strings.Join(names, ", ")
}

// oneRowJoins checks that the ON condition of each join equates every
// column of one unique key of the joined instance that orders every row
// (E31): else the join can return several rows per row before it, so the
// tiebreak orders no row and COUNT counts joined rows.
func (c *collector) oneRowJoins(s *searchDecl) {
	// An INNER JOIN can always start from the root. A root after another
	// table repeats once per row of that table, whatever its ON says.
	if s.reads[0] != s.root && !s.root.nullable {
		c.errorf(s.from.arg, "start the join from "+s.tableText,
			"search %s: From %s starts from %s: a row of %s repeats once per row before it",
			s.name, s.from.name, s.reads[0], s.root)
		return
	}
	for i, inst := range s.reads {
		if i == 0 {
			continue
		}
		equated := c.equated(s, inst, s.reads[:i])
		for _, k := range keysPrimaryFirst(inst.table) {
			if ordersEveryRow(inst.table, k) == "" && !slices.ContainsFunc(k.sqlCols, func(col string) bool {
				return !slices.Contains(equated, col)
			}) {
				inst.key = &k
				break
			}
		}
		if inst.key != nil {
			continue
		}
		c.errorf(s.from.arg, "join to one row: equate every column of a key that orders every row ("+
			keysOf(inst.table)+"), or filter on the child rows with nanorepo.FilterExists(...)",
			"search %s: %s joins %s ON %s, which equates no unique key of %s: a row of %s can repeat", s.name,
			s.from.name, inst, c.source(inst.on), inst, s.reads[0])
	}
}

// keysPrimaryFirst returns the keys of t with the primary key first.
func keysPrimaryFirst(t *tableBase) []uniqueKey {
	keys := slices.Clone(t.keys)
	slices.SortStableFunc(keys, func(a, b uniqueKey) int {
		return boolRank(isPrimaryKey(t, b)) - boolRank(isPrimaryKey(t, a))
	})
	return keys
}

// equated returns the columns of inst that a top-level AND conjunct of its
// ON condition equates with a column of an earlier instance, or with a
// field of the scope value of From(func(P) T).
func (c *collector) equated(s *searchDecl, inst *instance, earlier []*instance) []string {
	var out []string
	for _, conj := range c.conjuncts(inst.on) {
		call, isCall := ast.Unparen(conj).(*ast.CallExpr)
		if !isCall || len(call.Args) != 1 {
			continue
		}
		sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "EQ" || !c.dbMethod(sel) {
			continue
		}
		for _, pair := range [][2]ast.Expr{{sel.X, call.Args[0]}, {call.Args[0], sel.X}} {
			owner, col := c.onColumn(pair[0], []*instance{inst})
			if owner == nil {
				continue
			}
			if other, _ := c.onColumn(pair[1], earlier); other != nil || c.scopeField(s, pair[1]) {
				out = append(out, col)
			}
		}
	}
	return out
}

// conjuncts splits a condition into its top-level AND conjuncts: a.AND(b)
// and postgres.AND(a, b, ...). A conjunct under an OR stays whole.
func (c *collector) conjuncts(e ast.Expr) []ast.Expr {
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if !isCall {
		return []ast.Expr{e}
	}
	if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel && sel.Sel.Name == "AND" {
		if c.dbMethod(sel) && len(call.Args) == 1 {
			return append(c.conjuncts(sel.X), c.conjuncts(call.Args[0])...)
		}
		if v, isVar := c.l.pkg.TypesInfo.Uses[sel.Sel].(*types.Var); isVar && inDB(v.Pkg()) {
			var out []ast.Expr
			for _, a := range call.Args {
				out = append(out, c.conjuncts(a)...)
			}
			return out
		}
	}
	return []ast.Expr{e}
}

// onColumn resolves e to a column of one of insts: X.Col or
// X.AS("alias").Col. It returns the instance and the column name.
func (c *collector) onColumn(e ast.Expr, insts []*instance) (*instance, string) {
	sel, isSel := ast.Unparen(e).(*ast.SelectorExpr)
	if !isSel {
		return nil, ""
	}
	base, alias := ast.Unparen(sel.X), ""
	if as, isAS := c.aliasCall(base); isAS {
		tv := c.l.pkg.TypesInfo.Types[as.Args[0]]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			return nil, ""
		}
		alias, base = constant.StringVal(tv.Value), tableOperand(base)
	}
	v, _ := c.l.pkg.TypesInfo.Uses[tableIdent(base)].(*types.Var)
	if !c.tableVariable(v) {
		return nil, ""
	}
	t := c.searchTables[v]
	for _, inst := range insts {
		if col, found := inst.table.column(sel.Sel.Name); found && inst.is(t, alias) {
			return inst, col.sqlName
		}
	}
	return nil, ""
}

// scopeField reports whether e is a literal of a field of the scope value
// of From(func(P) T), such as postgres.String(s.PlatformTenantID).
func (c *collector) scopeField(s *searchDecl, e ast.Expr) bool {
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if s.from.param == nil || !isCall || len(call.Args) != 1 {
		return false
	}
	if obj := c.l.pkg.TypesInfo.Uses[tableIdent(ast.Unparen(call.Fun))]; obj == nil || !inDB(obj.Pkg()) {
		return false
	}
	field, isSel := ast.Unparen(call.Args[0]).(*ast.SelectorExpr)
	return isSel && c.isScopeVar(s, field.X)
}

// isScopeVar reports whether e names the parameter of the From function,
// or a parameter of a helper that the caller passes it to. A package
// variable or a value of the scope type that a function builds is not the
// scope of the caller.
func (c *collector) isScopeVar(s *searchDecl, e ast.Expr) bool {
	v, isVar := c.l.pkg.TypesInfo.Uses[identOf(ast.Unparen(e))].(*types.Var)
	return isVar && s.from.scopeVars[v]
}

// fkCondition renders the ON condition of a declared foreign key between
// a and b, in either direction, with the table variables as written.
func fkCondition(a *tableBase, aText string, b *tableBase, bText string) (string, bool) {
	render := func(from *tableBase, fromText string, to *tableBase, toText string, k foreignKey) (string, bool) {
		var out strings.Builder
		for i, col := range k.columns {
			f, okFrom := fieldOf(from, col)
			g, okTo := fieldOf(to, k.refColumns[i])
			if !okFrom || !okTo {
				return "", false
			}
			cond := fromText + "." + f + ".EQ(" + toText + "." + g + ")"
			if i > 0 {
				cond = ".AND(" + cond + ")"
			}
			out.WriteString(cond)
		}
		return out.String(), out.Len() > 0
	}
	for _, k := range a.foreignKeys {
		if k.references == b.sqlName && len(k.columns) > 0 {
			return render(a, aText, b, bText, k)
		}
	}
	for _, k := range b.foreignKeys {
		if k.references == a.sqlName && len(k.columns) > 0 {
			on, ok := render(b, bText, a, aText, k)
			return on, ok
		}
	}
	return "", false
}

// fieldOf returns the Go field of the column sqlName of t.
func fieldOf(t *tableBase, sqlName string) (string, bool) {
	for _, col := range t.columns {
		if col.sqlName == sqlName {
			return col.field, true
		}
	}
	return "", false
}
