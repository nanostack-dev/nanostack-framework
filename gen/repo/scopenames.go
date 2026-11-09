package genrepo

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// ctxScope is a context scope column of a table: Postgres.ContextScopes
// registers its function, and a short name from Postgres.ScopeNames makes a
// derived method apply it without a parameter (ADR 0026).
type ctxScope struct {
	field string // Go field of the column
	col   string // SQL column
	fn    string // import path, a dot and the function name
	short string // the short name, "" when ScopeNames gives none
}

// term is one property of a derived name. A term of a context scope named by
// its short name has ctx set: it binds no parameter, and the method ANDs the
// registered function of the column. Every other term binds one parameter.
type term struct {
	field   string
	ctx     *ctxScope
	fromRow bool // a write reads the column from its value: a key column, not a scope
}

// word is a word of a derived name that resolves to a column: its Go field,
// or the short name of a scope column.
type word struct{ text, field string }

// setupScopes reads the scope columns of the table of r: every scope column
// (nullable or not), the context ones, and the short names.
func (r *resolver) setupScopes(scopes []string) {
	r.short, r.shortOf = map[string]string{}, map[string]string{}
	var cols []column
	for _, c := range r.t.columns {
		if slices.Contains(scopes, c.sqlName) {
			cols = append(cols, c)
			r.allScope = append(r.allScope, c.field)
		}
	}
	for _, c := range cols {
		short := r.l.scopeNames[c.sqlName]
		if short == "" || short == c.field {
			continue
		}
		if other, isField := r.t.column(short); isField {
			r.clashes = append(r.clashes, fmt.Sprintf("ScopeNames names %s %q, which is the Go field of column %s "+
				"of table %s: a derived name could not tell them apart", c.sqlName, short, other.sqlName, r.t.sqlName))
			continue
		}
		if _, dup := r.short[short]; dup {
			r.clashes = append(r.clashes, fmt.Sprintf("ScopeNames gives the name %q to two columns of table %s",
				short, r.t.sqlName))
			continue
		}
		r.short[short], r.shortOf[c.field] = c.field, short
	}
	for _, c := range cols {
		if fn, ok := r.l.contextScopes[c.sqlName]; ok {
			r.ctx = append(r.ctx, ctxScope{field: c.field, col: c.sqlName, fn: fn, short: r.shortOf[c.field]})
		}
	}
}

// clashProblems are the refusals of a short name that a table cannot use.
func (r *resolver) clashProblems() []error {
	out := make([]error, 0, len(r.clashes))
	for _, msg := range r.clashes {
		out = append(out, gen.Errorf(r.b.file, r.b.line,
			"rename the column, or choose another name in Postgres.ScopeNames (nanostack gen repo -scope-names)",
			"%s", msg))
	}
	return out
}

// tokens are the words a derived name may use: the Go field of each column,
// then the short name of each scope column of the table.
func (r *resolver) words() []word {
	out := make([]word, 0, len(r.t.columns)+len(r.short))
	for _, f := range r.t.fields() {
		out = append(out, word{f, f})
	}
	shorts := make([]string, 0, len(r.short))
	for s := range r.short {
		shorts = append(shorts, s)
	}
	slices.Sort(shorts)
	for _, s := range shorts {
		if !slices.ContainsFunc(out, func(t word) bool { return t.text == s }) {
			out = append(out, word{s, r.short[s]})
		}
	}
	return out
}

// termsOf turns the tokens of a name into terms.
func (r *resolver) termsOf(toks []word) []term {
	out := make([]term, len(toks))
	for i, t := range toks {
		out[i] = term{field: t.field}
		if t.text != t.field {
			out[i].ctx = r.ctxOf(t.field)
		}
	}
	return out
}

func (r *resolver) ctxOf(field string) *ctxScope {
	for i := range r.ctx {
		if r.ctx[i].field == field {
			return &r.ctx[i]
		}
	}
	return nil
}

// isHardScope reports whether t satisfies the rule that a name starts with a
// scope column. A context term does so only when its column is the table's
// one scope: a context scope can return TRUE, so it never stands for a
// tenant that the caller passes.
func (r *resolver) isHardScope(t term) bool {
	return slices.Contains(r.scope, t.field) && (t.ctx == nil || len(r.allScope) == 1)
}

// missingCtx returns the context scope columns of the table that the terms
// do not name, by either form.
func (r *resolver) missingCtx(terms []term) []ctxScope {
	var out []ctxScope
	for _, c := range r.ctx {
		if !slices.ContainsFunc(terms, func(t term) bool { return t.field == c.field }) {
			out = append(out, c)
		}
	}
	return out
}

// display is the name of a column in a suggested method name: its short
// name when ScopeNames gives one, else its Go field.
func (r *resolver) display(field string) string {
	if s := r.shortOf[field]; s != "" {
		return s
	}
	return field
}

// displayTerm is the word of t in a suggested name. A context column keeps
// the form of its term: the short name binds no parameter, the field does.
func (r *resolver) displayTerm(t term) string {
	if r.ctxOf(t.field) != nil {
		if t.ctx != nil {
			return t.ctx.short
		}
		return t.field
	}
	return r.display(t.field)
}

// scopeOrder puts the terms of a name in the order the scope rules ask for:
// the hard scope first, then the context scopes, then the rest. A context
// scope the name lacks joins by its short name when it has one, else by its
// field, which takes a parameter.
func (r *resolver) scopeOrder(terms []term) []string {
	var out []string
	used := map[string]bool{}
	add := func(field, word string) {
		if !used[field] {
			used[field] = true
			out = append(out, word)
		}
	}
	hard := ""
	for _, t := range terms {
		if slices.Contains(r.scope, t.field) && r.ctxOf(t.field) == nil {
			hard = t.field
			break
		}
	}
	if hard == "" {
		for _, f := range r.scope {
			if r.ctxOf(f) == nil {
				hard = f
				break
			}
		}
	}
	if hard != "" {
		add(hard, r.display(hard))
	}
	for _, c := range r.ctx {
		switch at := slices.IndexFunc(terms, func(t term) bool { return t.field == c.field }); {
		case at >= 0:
			add(c.field, r.displayTerm(terms[at]))
		case c.short != "":
			add(c.field, c.short)
		default:
			add(c.field, c.field)
		}
	}
	for _, t := range terms {
		add(t.field, r.displayTerm(t))
	}
	return out
}

// suggestNamed builds a derived name from base with names as its properties:
// FindByEmail and [Org Email] -> FindByOrgAndEmail; FindAll and [Org] ->
// FindByOrg.
func suggestNamed(base string, names []string) string {
	prefix := ""
	switch base {
	case "Create":
		prefix = "CreateIn"
	case "FindAll":
		prefix = "FindBy"
	case "Count":
		prefix = "CountBy"
	default:
		for _, p := range prefixes {
			if strings.HasPrefix(base, p.name) {
				prefix = p.name
				break
			}
		}
	}
	return prefix + strings.Join(names, "And")
}

// funcWord is how a generated file names a registered function: the last
// element of its import path, a dot and the name (workspacescope.Filter).
func funcWord(fn string) string {
	dot := strings.LastIndex(fn, ".")
	if dot < 0 {
		return fn
	}
	return path.Base(fn[:dot]) + fn[dot:]
}
