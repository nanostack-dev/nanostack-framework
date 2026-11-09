package genrepo

import (
	"cmp"
	"fmt"
	"go/types"
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

// ctxOnly reports whether the table has context scopes and no hard scope: a
// bare context then reads TRUE for every one of them, a global read.
func (r *resolver) ctxOnly() bool {
	return len(r.ctx) > 0 && len(r.ctx) == len(r.allScope)
}

// failClosed sets the Present guards of m, and refuses what would fail open
// without a reviewed choice. A delete or an update that changes rows, and a
// read of a table that has no hard scope, must find the context scope in ctx:
// with Present the method checks it first; without, the method is refused,
// unless ContextScope.AllowFailOpenWrites says a delete or update may (the
// reviewed choice, which the comment and -inventory show), or the read ends in
// Internal. Reads of a table with a hard scope keep the fail-open filter.
func (r *resolver) failClosed(fn *types.Func, m *derivedMethod) *refusal {
	read := !m.op.changesRows()
	if read && (!r.ctxOnly() || m.internal) {
		return nil
	}
	for _, c := range r.ctx {
		switch {
		case read && slices.Contains(m.cross, c.col):
			// the name binds the column from a parameter: ctx is not read.
		case c.present != "":
			m.present = append(m.present, c)
		case !read && (m.op == opCreate || m.op == opUpsert):
			// the stamp of a create or an upsert fails without a value: closed.
		case !read && c.failOpenOK:
			m.failOpen = append(m.failOpen, c)
		default:
			return r.failOpenRefusal(fn, m, c, read)
		}
	}
	return nil
}

// failOpenRefusal is the refusal of a method that would fail open.
func (r *resolver) failOpenRefusal(_ *types.Func, m *derivedMethod, c ctxScope, read bool) *refusal {
	if read {
		return &refusal{
			msg: fmt.Sprintf("table %s has the context scope %s as its only scope, and its function reads TRUE on "+
				"a context without a value: a derived read would read every tenant", r.t.sqlName, c.col),
			alt: "set Present in the ContextScope of " + c.col + " in nanogen/project.go (the read then fails " +
				"with nanorepo.ErrNoContextScope on a bare context), or end the name in Internal for a trusted " +
				"system path"}
	}
	return &refusal{
		msg: fmt.Sprintf("a derived %s on table %s with the context scope %s fails open: on a context without a "+
			"value its function reads TRUE, and it would change every value of %s", m.op, r.t.sqlName, c.col, c.col),
		alt: "set Present in the ContextScope of " + c.col + " in nanogen/project.go (the method then fails with " +
			"nanorepo.ErrNoContextScope), or set AllowFailOpenWrites there to record that a write without it is " +
			"reviewed, or write it by hand"}
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
