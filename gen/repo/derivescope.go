package genrepo

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// collectPreds records the parameter-bound predicates of m, and the context
// columns a name binds from a parameter (m.cross).
func (r *resolver) collectPreds(m *derivedMethod) {
	for i, t := range m.terms {
		switch {
		case t.ctx != nil:
		case m.op.isWrite() && !slices.Contains(r.allScope, t.field):
			m.terms[i].fromRow = true // a key column: the write reads it from its value
		default:
			m.preds = append(m.preds, t.field)
			if c := r.ctxOf(t.field); c != nil {
				m.cross = append(m.cross, c.col)
			}
		}
	}
}

// crossRefusal is the refusal of a delete or a write that names a context
// scope column by its Go field (WorkspaceID): the field binds a parameter and
// skips the registered function, a cross-workspace read that only a read may
// do.
func (r *resolver) crossRefusal(m *derivedMethod) *refusal {
	var fields, shorts []string
	for _, t := range m.terms {
		if c := r.ctxOf(t.field); c != nil && t.ctx == nil {
			fields = append(fields, t.field)
			shorts = append(shorts, cmp.Or(c.short, "its short name from Postgres.ScopeNames"))
		}
	}
	what := "delete"
	if m.op.isWrite() {
		what = "write"
	}
	return &refusal{
		msg: fmt.Sprintf("a derived %s names the context scope %s by its Go field, which binds a parameter and "+
			"skips the function of Postgres.ContextScopes: a cross-workspace %s is hand-written, where review "+
			"sees it", what, strings.Join(fields, " and "), what),
		alt: "name " + strings.Join(shorts, " and ") + " instead, which applies the function of the context"}
}

// impliesHint is the part of a scope refusal that says what Postgres.ScopeImplies
// could do for the missing hard columns: nothing on a write, and on a read
// either the foreign key that the table lacks or the entry to declare.
func (r *resolver) impliesHint(m *derivedMethod, missing []string) string {
	hardMissing := slices.ContainsFunc(missing, func(f string) bool { return r.ctxOf(f) == nil })
	namesScope := slices.ContainsFunc(m.terms, func(t term) bool { return slices.Contains(r.allScope, t.field) })
	if !hardMissing || !namesScope || m.op.isWrite() {
		return ""
	}
	if why := r.unprovenImplication(m.terms, missing); why != "" {
		return "; " + why
	}
	return "; if a column the name has determines the others in the schema, declare it in " +
		"Postgres.ScopeImplies, and give the table the foreign key that proves it"
}
