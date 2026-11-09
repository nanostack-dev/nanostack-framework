package genrepo

import (
	"go/ast"
	"go/types"
	"path"
	"slices"
	"strings"
)

// tenantNode is one table instance of the tenant check (design S6.2): an
// instance of the read set, or an instance that a subquery reads.
type tenantNode struct {
	inst  *instance
	level int      // 0: the read set; n: the n-th subquery
	cond  ast.Expr // the ON condition, or the WHERE of the first instance of a subquery
	// filters is true when cond filters every instance of its level: an
	// INNER JOIN, or the WHERE of a subquery. The ON of a LEFT JOIN filters
	// only its own instance: the rows before it exist without it (rule 5).
	filters bool
	role    string // the function of a subquery, "" for the read set
	first   bool   // the first instance of a subquery: cond correlates it
	tenant  []string
	how     map[string]string // covered tenant column -> the rule that covers it
	exempt  map[string]string // Unscoped tenant column -> why
	linked  string            // rule 4: how an instance with no tenant column is covered
	rule3   string            // rule 3: the key that covers the instance, for the exemption file
}

// covered reports whether the instance is covered: every tenant column
// covered or exempted, with one covered at least; or, with no tenant
// column, linked to a covered instance.
func (n *tenantNode) covered() bool {
	if len(n.tenant) == 0 {
		return n.linked != ""
	}
	any := false
	for _, col := range n.tenant {
		switch {
		case n.how[col] != "":
			any = true
		case n.exempt[col] == "":
			return false
		}
	}
	return any
}

// tenantSide is one side of an equality: a column of an instance, or a
// field of the scope value of From(func(P) T).
type tenantSide struct {
	node  *tenantNode
	col   string
	field string
}

// tenantEq is one top-level AND conjunct a.EQ(b) of the condition of in.
type tenantEq struct {
	a, b tenantSide
	in   *tenantNode
}

// tenantCheck proves that a search reads only the rows of one tenant.
type tenantCheck struct {
	c     *collector
	s     *searchDecl
	nodes []*tenantNode
	eqs   []tenantEq
	// links are the correlations of FilterExists, child.link = root key,
	// whose root side linkRefs names: the subquery is written from the
	// declared foreign key, not from Go source.
	links    []tenantEq
	linkRefs []*colRef
}

// checkTenant checks the tenant coverage of a search (design S6): the kind
// of each scope (E9, E12, E29), the subqueries (E8), the coverage of every
// instance and tenant column (E8, E9, E10) and the exemptions (E30). It
// runs only when Postgres.Scopes names tenant columns.
func (c *collector) checkTenant(s *searchDecl) {
	if len(c.l.scopes) == 0 {
		return
	}
	t := &tenantCheck{c: c, s: s}
	t.nodes = t.buildNodes()
	before := len(c.problems)
	t.checkScopes()
	t.checkInnerNames()
	if len(c.problems) > before {
		return
	}
	t.readEqualities()
	for t.step() || t.rule3() {
	}
	t.checkUnscoped()
	t.checkCovered()
	if len(c.problems) > before {
		return
	}
	t.record()
}

// buildNodes makes one node per instance of the read set, then per
// instance of each subquery, with its tenant columns.
func (t *tenantCheck) buildNodes() []*tenantNode {
	var nodes []*tenantNode
	add := func(n *tenantNode) {
		for _, col := range n.inst.table.columns {
			if slices.Contains(t.c.l.scopes, col.sqlName) {
				n.tenant = append(n.tenant, col.sqlName)
			}
		}
		n.how, n.exempt = map[string]string{}, map[string]string{}
		nodes = append(nodes, n)
	}
	for _, inst := range t.s.reads {
		add(&tenantNode{inst: inst, cond: inst.on, filters: inst.join == "INNER JOIN"})
	}
	level := 0
	for _, in := range t.s.inner {
		if in.inst.join == "" { // the first table of a subquery's FROM starts a subquery
			level++
		}
		n := &tenantNode{inst: in.inst, level: level, role: in.role, cond: in.inst.on,
			filters: in.inst.join == "INNER JOIN"}
		if in.inst.join == "" {
			n.first, n.cond, n.filters = true, in.where, true
		}
		add(n)
	}
	for _, f := range t.s.filters {
		if f.kind != "FilterExists" || f.ref == nil {
			continue
		}
		level++
		n := &tenantNode{inst: &instance{table: f.child, text: f.child.goName, node: f.link.expr}, level: level,
			role: "FilterExists " + f.name, first: true, filters: true}
		add(n)
		t.links = append(t.links, tenantEq{a: tenantSide{node: n, col: f.link.col.sqlName},
			b: tenantSide{col: f.ref.col.sqlName}, in: n})
		t.linkRefs = append(t.linkRefs, f.ref)
	}
	return nodes
}

// outerNode returns the node of the read set that col names.
func (t *tenantCheck) outerNode(col *colRef) *tenantNode {
	for _, n := range t.nodes {
		if n.level == 0 && n.inst.is(col.table, col.alias) {
			return n
		}
	}
	return nil
}

// isContext reports whether col is a context tenant column.
func (t *tenantCheck) isContext(col string) bool {
	_, found := t.c.l.contextScopes[col]
	return found
}

// checkScopes checks the kind of each scope (E9), a column scoped twice,
// scoped and Unscoped, or two scopes of one field name (E12), a scope on
// the nullable side of a LEFT JOIN (E29), and Unscoped on a column that is
// no tenant column. Then it marks the columns that the scopes cover (rule 1)
// and the exempted ones.
func (t *tenantCheck) checkScopes() {
	s, c := t.s, t.c
	seen := map[*tenantNode]map[string]string{} // node -> column -> the option that names it
	name := func(n *tenantNode, col *colRef, option string) bool {
		if seen[n] == nil {
			seen[n] = map[string]string{}
		}
		if first := seen[n][col.col.sqlName]; first != "" {
			c.errorf(col.expr, "name each column in one scope option",
				"search %s: %s(%s) names %s.%s, which %s names already", s.name, option, col, n.inst.name(),
				col.col.sqlName, first)
			return false
		}
		seen[n][col.col.sqlName] = option
		return true
	}
	fields := map[string]*colRef{}
	for _, sc := range s.scopes {
		t.checkScopeTypes(sc)
		if sc.kind != "ScopeNull" && len(sc.cols) > 0 {
			t.checkFieldName(sc, fields)
		}
		for _, col := range sc.cols {
			n := t.outerNode(col)
			if !t.direct(n, col, sc.kind) || !name(n, col, sc.kind) {
				continue
			}
			switch {
			case t.isContext(col.col.sqlName):
				c.errorf(col.expr, "nanorepo.ScopeFunc("+t.registeredName(col.col.sqlName)+", "+col.String()+")",
					"search %s: %s(%s): %s is a context tenant column (Postgres.ContextScopes): only its "+
						"registered ScopeFunc scopes it", s.name, sc.kind, col, col.col.sqlName)
			case slices.Contains(n.tenant, col.col.sqlName):
				n.how[col.col.sqlName] = sc.kind
			}
		}
	}
	for _, sf := range s.scopeFuncs {
		n := t.outerNode(sf.col)
		if t.direct(n, sf.col, "ScopeFunc") && name(n, sf.col, "ScopeFunc") && t.checkScopeFunc(sf) {
			n.how[sf.col.col.sqlName] = "ScopeFunc " + c.source(sf.fn)
		}
	}
	for _, u := range s.unscoped {
		n := t.outerNode(u.col)
		if !slices.Contains(n.tenant, u.col.col.sqlName) {
			c.errorf(u.col.expr, "remove Unscoped: only a tenant column of Postgres.Scopes is scoped",
				"search %s: Unscoped(%s): %s is not a tenant column, so there is nothing to exempt", s.name, u.col,
				u.col.col.sqlName)
			continue
		}
		if name(n, u.col, "Unscoped") {
			n.exempt[u.col.col.sqlName] = u.why
		}
	}
}

// direct checks that a direct scope is not on the nullable side of a LEFT
// JOIN (E29): its WHERE condition would drop the rows that have no match.
func (t *tenantCheck) direct(n *tenantNode, col *colRef, option string) bool {
	if !n.inst.nullable {
		return true
	}
	t.c.errorf(col.expr, "remove it, and equate the column in the ON clause with a covered tenant column, "+
		"or put the value in the ON clause with nanorepo.From(func(P) T)",
		"search %s: %s(%s) is a WHERE condition on %s, on the nullable side of a LEFT JOIN: it drops the "+
			"rows of %s that have no %s", t.s.name, option, col, n.inst, t.s.root, n.inst)
	return false
}

// checkScopeTypes checks that the columns of one Scope take one type: one
// field gives its value to each of them.
func (t *tenantCheck) checkScopeTypes(sc scopeDecl) {
	for _, col := range sc.cols[min(1, len(sc.cols)):] {
		first := sc.cols[0]
		if !types.Identical(col.col.value, first.col.value) {
			t.c.errorf(col.expr, "scope "+col.String()+" in a Scope of its own",
				"search %s: %s(%s, %s): one field gives one value to each column, and %s is %s, %s is %s",
				t.s.name, sc.kind, first, col, first.col.sqlName, t.c.qualified(first.col.value), col.col.sqlName,
				t.c.qualified(col.col.value))
			return
		}
	}
}

// checkFieldName checks that two scopes do not make one field of the scope
// value (E12): a child and a parent column of one name share one field.
func (t *tenantCheck) checkFieldName(sc scopeDecl, fields map[string]*colRef) {
	col := sc.cols[0]
	first, dup := fields[col.col.field]
	if !dup {
		fields[col.col.field] = col
		return
	}
	t.c.errorf(col.expr, "nanorepo.Scope("+first.String()+", "+col.String()+") gives one value to both",
		"search %s: Scope(%s) and %s(%s) both make the field %s", t.s.name, first, sc.kind, col, col.col.field)
}

// checkScopeFunc checks a ScopeFunc: on a context tenant column, with the
// function that Postgres.ContextScopes registers for it (E9).
func (t *tenantCheck) checkScopeFunc(sf scopeFuncDecl) bool {
	s, c, col := t.s, t.c, sf.col
	switch {
	case !slices.Contains(c.l.scopes, col.col.sqlName):
		c.errorf(sf.fn, "scope "+col.String()+" with nanorepo.Scope, or filter on it with nanorepo.Condition",
			"search %s: ScopeFunc(%s, %s): %s is not a context tenant column (Postgres.ContextScopes)", s.name,
			c.source(sf.fn), col, col.col.sqlName)
		return false
	case !t.isContext(col.col.sqlName):
		c.errorf(sf.fn, "nanorepo.Scope("+col.String()+"), whose value the caller passes",
			"search %s: ScopeFunc(%s, %s): %s is a hard tenant column: its value comes from the scope value, "+
				"never from a function, which could return TRUE", s.name, c.source(sf.fn), col, col.col.sqlName)
		return false
	}
	want := c.l.contextScopes[col.col.sqlName]
	fn, _ := c.l.pkg.TypesInfo.Uses[tableIdent(ast.Unparen(sf.fn))].(*types.Func)
	if fn != nil && fn.Pkg() != nil && fn.Pkg().Path()+"."+fn.Name() == want {
		return true
	}
	c.errorf(sf.fn, "nanorepo.ScopeFunc("+t.registeredName(col.col.sqlName)+", "+col.String()+")",
		"search %s: ScopeFunc(%s, %s): %s is not the function that Postgres.ContextScopes registers for %s (%s)",
		s.name, c.source(sf.fn), col, c.source(sf.fn), col.col.sqlName, want)
	return false
}

// registeredName is the registered function of a context column, as the
// declaring package names it: workspacescope.Filter, or Filter in its own
// package.
func (t *tenantCheck) registeredName(col string) string {
	full := t.c.l.contextScopes[col]
	dot := strings.LastIndex(full, ".")
	if dot < 0 {
		return full
	}
	if full[:dot] == t.c.l.pkg.PkgPath {
		return full[dot+1:]
	}
	return path.Base(full[:dot]) + full[dot:]
}

// checkInnerNames refuses a subquery instance with the name of an instance
// of the read set (E8): SQL binds both sides of a correlation to the inner
// instance, so inner.x = outer.x is always true.
func (t *tenantCheck) checkInnerNames() {
	for _, n := range t.nodes {
		if n.level == 0 {
			continue
		}
		at := slices.IndexFunc(t.s.reads, func(i *instance) bool { return i.name() == n.inst.name() })
		if at < 0 {
			continue
		}
		t.c.errorf(n.inst.node, "give it an alias: "+n.inst.text+`.AS("inner_`+n.inst.table.sqlName+`")`,
			"%s: the subquery reads %s, which has the name of an instance of search %s: SQL binds a "+
				"correlation on it to the inner row, so it compares the row with itself", n.role, n.inst.name(),
			t.s.name)
	}
}

// readEqualities reads the top-level AND conjuncts a.EQ(b) of each ON
// condition and correlated WHERE. A conjunct under an OR counts for nothing.
func (t *tenantCheck) readEqualities() {
	for i, eq := range t.links {
		eq.b.node = t.outerNode(t.linkRefs[i])
		t.eqs = append(t.eqs, eq)
	}
	for _, n := range t.nodes {
		if n.cond == nil {
			continue
		}
		var scope []*tenantNode // the instances a column of the condition can name, inner first
		for _, m := range t.nodes {
			if m.level == n.level && m.level > 0 {
				scope = append(scope, m)
			}
		}
		for _, m := range t.nodes {
			if m.level == 0 {
				scope = append(scope, m)
			}
		}
		for _, conj := range t.c.conjuncts(n.cond) {
			call, isCall := ast.Unparen(conj).(*ast.CallExpr)
			if !isCall || len(call.Args) != 1 {
				continue
			}
			sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !isSel || sel.Sel.Name != "EQ" || !t.c.dbMethod(sel) {
				continue
			}
			a, b := t.side(sel.X, scope, n), t.side(call.Args[0], scope, n)
			if (a.node != nil || a.field != "") && (b.node != nil || b.field != "") {
				t.eqs = append(t.eqs, tenantEq{a: a, b: b, in: n})
			}
		}
	}
}

// side resolves one side of an equality to a column of an instance, or to
// a field of the scope value in the ON clause of a From function.
func (t *tenantCheck) side(e ast.Expr, scope []*tenantNode, in *tenantNode) tenantSide {
	insts := make([]*instance, len(scope))
	for i, n := range scope {
		insts[i] = n.inst
	}
	if inst, col := t.c.onColumn(e, insts); inst != nil {
		return tenantSide{node: scope[slices.Index(insts, inst)], col: col}
	}
	if in.level == 0 && t.s.from != nil && t.c.scopeField(t.s, e) {
		if field, isSel := ast.Unparen(ast.Unparen(e).(*ast.CallExpr).Args[0]).(*ast.SelectorExpr); isSel {
			return tenantSide{field: field.Sel.Name}
		}
		return tenantSide{field: singleField(t.s)}
	}
	return tenantSide{}
}

// can reports whether an equality in the condition of in can cover target:
// a condition covers its own instance, and an INNER JOIN or a subquery's
// WHERE also covers the other instances of its level (rule 5).
func can(target, in *tenantNode) bool {
	return target == in || (in.filters && target.level == in.level)
}

// step applies rules 2 and 4 once to every equality and every pair of
// instances, and reports whether a column or an instance became covered.
func (t *tenantCheck) step() bool {
	changed := false
	for _, eq := range t.eqs {
		for _, p := range [][2]tenantSide{{eq.a, eq.b}, {eq.b, eq.a}} {
			src, dst := p[0], p[1]
			if dst.node == nil || !can(dst.node, eq.in) || !slices.Contains(dst.node.tenant, dst.col) ||
				dst.node.how[dst.col] != "" {
				continue
			}
			switch {
			case src.node != nil && src.col == dst.col && src.node.how[src.col] != "":
				dst.node.how[dst.col] = "equated with " + src.node.inst.name() + "." + src.col
				changed = true
			case src.field != "" && t.fieldCovers(src.field, dst.col):
				dst.node.how[dst.col] = "equated with the scope field " + src.field
				changed = true
			}
		}
	}
	t.eachPair(func(x, y, in *tenantNode) {
		if !x.covered() || !can(y, in) {
			return
		}
		if k, ok := t.fkMatch(y, x, in); ok { // rule 2: y references the covered x by its tenant column
			for _, col := range k.columns {
				if slices.Contains(y.tenant, col) && y.how[col] == "" {
					y.how[col] = "references " + x.inst.name() + " by " + k.name
					changed = true
				}
			}
		}
		if len(y.tenant) > 0 || y.linked != "" { // rule 4
			return
		}
		k, ok := t.fkMatch(y, x, in)
		if !ok {
			k, ok = t.fkMatch(x, y, in)
		}
		if ok {
			y.linked = "linked to " + x.inst.name() + " by " + k.name
			changed = true
		}
	})
	return changed
}

// rule3 covers every tenant column of one instance that a covered instance
// references by a declared foreign key: the covered row's own data. It runs
// only when rules 2 and 4 cover nothing more, so the exemption file lists
// only the coverage that needs it.
func (t *tenantCheck) rule3() bool {
	changed := false
	t.eachPair(func(x, y, in *tenantNode) {
		if changed || len(y.tenant) == 0 || y.covered() || !x.covered() || !can(y, in) {
			return
		}
		k, ok := t.fkMatch(x, y, in)
		if !ok {
			return
		}
		y.rule3 = x.inst.name() + " references it by " + k.name
		for _, col := range y.tenant {
			if y.how[col] == "" && y.exempt[col] == "" {
				y.how[col] = "rule 3: " + y.rule3
			}
		}
		changed = true
	})
	return changed
}

// eachPair calls fn for each ordered pair of distinct instances that an
// equality of one condition links, with that condition's instance.
func (t *tenantCheck) eachPair(fn func(x, y, in *tenantNode)) {
	for _, in := range t.nodes {
		var linked []*tenantNode
		for _, eq := range t.eqs {
			if eq.in != in {
				continue
			}
			for _, side := range []tenantSide{eq.a, eq.b} {
				if side.node != nil && !slices.Contains(linked, side.node) {
					linked = append(linked, side.node)
				}
			}
		}
		for _, x := range linked {
			for _, y := range linked {
				if x != y {
					fn(x, y, in)
				}
			}
		}
	}
}

// fkMatch returns a foreign key of from to to whose every column pair an
// equality of the condition of in equates.
func (t *tenantCheck) fkMatch(from, to, in *tenantNode) (foreignKey, bool) {
	for _, k := range from.inst.table.foreignKeys {
		if k.references != to.inst.table.sqlName || (k.refSchema != "" && k.refSchema != to.inst.table.schema) ||
			len(k.columns) == 0 {
			continue
		}
		all := true
		for i, col := range k.columns {
			all = all && t.equates(in, from, col, to, k.refColumns[i])
		}
		if all {
			return k, true
		}
	}
	return foreignKey{}, false
}

// equates reports whether an equality of the condition of in equates
// a.colA with b.colB.
func (t *tenantCheck) equates(in, a *tenantNode, colA string, b *tenantNode, colB string) bool {
	for _, eq := range t.eqs {
		if eq.in != in {
			continue
		}
		if (eq.a.node == a && eq.a.col == colA && eq.b.node == b && eq.b.col == colB) ||
			(eq.b.node == a && eq.b.col == colA && eq.a.node == b && eq.a.col == colB) {
			return true
		}
	}
	return false
}

// fieldCovers reports whether the scope field field is the field of a
// Scope that names a column col: a tenant column equated with a field of
// another column is not covered.
func (t *tenantCheck) fieldCovers(field, col string) bool {
	for _, sc := range t.s.scopes {
		if sc.kind == "ScopeNull" || len(sc.cols) == 0 || sc.cols[0].col.field != field {
			continue
		}
		for _, c := range sc.cols {
			if c.col.sqlName == col {
				return true
			}
		}
	}
	return false
}
