package genrepo

import (
	"go/ast"
	"slices"
)

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
		scope := t.visible(n)
		for _, conj := range t.c.conjuncts(n.cond) {
			if eq, ok := t.equality(conj, scope, n); ok {
				t.eqs = append(t.eqs, eq)
			}
		}
	}
}

// visible returns the instances a column of the condition of n can name:
// the instances of its subquery first, then the read set.
func (t *tenantCheck) visible(n *tenantNode) []*tenantNode {
	var scope []*tenantNode
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
	return scope
}

// equality reads one conjunct a.EQ(b) whose sides are columns of the
// instances in scope or fields of the scope value.
func (t *tenantCheck) equality(conj ast.Expr, scope []*tenantNode, n *tenantNode) (tenantEq, bool) {
	call, isCall := ast.Unparen(conj).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return tenantEq{}, false
	}
	sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "EQ" || !t.c.dbMethod(sel) {
		return tenantEq{}, false
	}
	a, b := t.side(sel.X, scope, n), t.side(call.Args[0], scope, n)
	if (a.node == nil && a.field == "") || (b.node == nil && b.field == "") {
		return tenantEq{}, false
	}
	return tenantEq{a: a, b: b, in: n}, true
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
			changed = t.equate(p[0], p[1], eq.in) || changed
		}
	}
	return t.byKeys() || changed
}

// equate covers the tenant column dst when an equality of the condition of
// in equates it with a covered tenant column of the same name, or with the
// scope field of its Scope (rule 2).
func (t *tenantCheck) equate(src, dst tenantSide, in *tenantNode) bool {
	if dst.node == nil || !can(dst.node, in) || !slices.Contains(dst.node.tenant, dst.col) ||
		dst.node.how[dst.col] != "" {
		return false
	}
	switch {
	case src.node != nil && src.col == dst.col && src.node.how[src.col] != "":
		dst.node.how[dst.col] = "equated with " + src.node.inst.name() + "." + src.col
		return true
	case src.field != "" && t.fieldCovers(src.field, dst.col):
		dst.node.how[dst.col] = "equated with the scope field " + src.field
		return true
	}
	return false
}

// byKeys applies the foreign key forms of rule 2 and rule 4 once to every
// pair of instances that an equality links.
func (t *tenantCheck) byKeys() bool {
	changed := false
	t.eachPair(func(x, y, in *tenantNode) {
		if x.covered() && can(y, in) {
			changed = t.referenced(x, y, in) || changed
			changed = t.linked(x, y, in) || changed
		}
	})
	return changed
}

// referenced covers each tenant column of y that references the covered x
// by a declared foreign key (rule 2).
func (t *tenantCheck) referenced(x, y, in *tenantNode) bool {
	k, ok := t.fkMatch(y, x, in)
	if !ok {
		return false
	}
	changed := false
	for _, col := range k.columns {
		if slices.Contains(y.tenant, col) && y.how[col] == "" {
			y.how[col] = "references " + x.inst.name() + " by " + k.name
			changed = true
		}
	}
	return changed
}

// linked covers y, with no tenant column, when a declared foreign key in
// either direction links it to the covered x (rule 4).
func (t *tenantCheck) linked(x, y, in *tenantNode) bool {
	if len(y.tenant) > 0 || y.linked != "" {
		return false
	}
	k, ok := t.fkMatch(y, x, in)
	if !ok {
		k, ok = t.fkMatch(x, y, in)
	}
	if ok {
		y.linked = "linked to " + x.inst.name() + " by " + k.name
	}
	return ok
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
		linked := t.linkedIn(in)
		for _, x := range linked {
			for _, y := range linked {
				if x != y {
					fn(x, y, in)
				}
			}
		}
	}
}

// linkedIn returns the instances that the equalities of the condition of
// in name.
func (t *tenantCheck) linkedIn(in *tenantNode) []*tenantNode {
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
	return linked
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
