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
	// present is the function that reports whether ctx carries the column
	// (ContextScope.Present), "" when none is registered.
	present string
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
	r.short, r.shortOf, r.implied = map[string]string{}, map[string]string{}, map[string][]string{}
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
	for _, f := range r.allScope {
		r.implied[f] = r.impliedOf(f)
	}
	for _, c := range cols {
		if fn, ok := r.l.contextScopes[c.sqlName]; ok {
			r.ctx = append(r.ctx, ctxScope{field: c.field, col: c.sqlName, fn: fn, short: r.shortOf[c.field],
				present: r.l.contextPresents[c.sqlName]})
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

// covered returns the scope fields that the terms of a name stand for: each
// scope column it names, in either form, and the columns that these imply.
func (r *resolver) covered(terms []term, implied bool) map[string]bool {
	out := map[string]bool{}
	for _, t := range terms {
		if !slices.Contains(r.allScope, t.field) {
			continue
		}
		out[t.field] = true
		if !implied {
			continue
		}
		for _, f := range r.implied[t.field] {
			out[f] = true
		}
	}
	return out
}

// missingScopes returns the scope columns of the table that the terms do
// not stand for, in the table's column order: every scope column must be
// named, hard or context, nullable or not, unless a named column implies it
// (Postgres.ScopeImplies).
func (r *resolver) missingScopes(terms []term) []string { return r.missingOf(terms, true) }

// missingFor is missingScopes for the method m: a write never relaxes the
// rule with Postgres.ScopeImplies, so its name gives every scope column
// itself and none comes from the value.
func (r *resolver) missingFor(m *derivedMethod) []string {
	return r.missingOf(m.terms, !m.op.isWrite())
}

func (r *resolver) missingOf(terms []term, implied bool) []string {
	have := r.covered(terms, implied)
	var out []string
	for _, f := range r.allScope {
		if !have[f] {
			out = append(out, f)
		}
	}
	return out
}

// misordered reports whether a column that is no scope column comes before a
// scope column: the scope words come first, so that the parameters of the
// tenant lead.
func (r *resolver) misordered(terms []term) bool {
	other := false
	for _, t := range terms {
		if !slices.Contains(r.allScope, t.field) {
			other = true
		} else if other {
			return true
		}
	}
	return false
}

// impliedOf returns the scope fields that field implies, transitively: each
// step is an entry of Postgres.ScopeImplies that this table proves with a
// foreign key (see proves). An entry that the table does not prove implies
// nothing here.
func (r *resolver) impliedOf(field string) []string {
	var out []string
	seen := map[string]bool{field: true}
	col := r.sqlName(field)
	for {
		next, ok := r.l.scopeImplies[col]
		if !ok || seen[next] {
			return out
		}
		if !r.t.proves(col, next) {
			return out
		}
		seen[next] = true
		col = next
		for _, f := range r.allScope {
			if r.sqlName(f) == next {
				out = append(out, f)
			}
		}
	}
}

// proves reports whether the table proves that column a determines column
// b, and a foreign key of the table lists
// both among its referencing columns, and the referenced table has a unique
// key (not partial) on the column that a references, alone. A row with a
// given a then has one b, because the referenced row has one.
func (t *tableBase) proves(a, b string) bool {
	if t.src == nil {
		return false
	}
	for _, k := range t.foreignKeys {
		ia, ib := slices.Index(k.columns, a), slices.Index(k.columns, b)
		if ia < 0 || ib < 0 || len(k.refColumns) != len(k.columns) {
			continue
		}
		for name, names := range t.src.tables {
			if names.table != k.references || (k.refSchema != "" && names.schema != k.refSchema) {
				continue
			}
			for _, key := range t.src.keys[name+"Table"] {
				if len(key.sqlCols) == 1 && key.sqlCols[0] == k.refColumns[ia] && key.pred == "" {
					return true
				}
			}
		}
	}
	return false
}

// unprovenImplication explains an entry of Postgres.ScopeImplies that would
// cover the missing columns for the named ones, and that this table does not
// prove. It returns "" when no entry applies.
func (r *resolver) unprovenImplication(terms []term, missing []string) string {
	for _, t := range terms {
		if !slices.Contains(r.allScope, t.field) {
			continue
		}
		col := r.sqlName(t.field)
		seen := map[string]bool{col: true}
		for {
			next, ok := r.l.scopeImplies[col]
			if !ok || seen[next] {
				break
			}
			seen[next] = true
			if !r.t.proves(col, next) {
				if slices.ContainsFunc(missing, func(f string) bool { return r.sqlName(f) == next }) {
					return fmt.Sprintf("Postgres.ScopeImplies says %s determines %s, but table %s has no foreign key "+
						"that lists both and references a table with a unique key on the target of %s alone, "+
						"so the implication does not apply here", col, next, r.t.sqlName, col)
				}
				break
			}
			col = next
		}
	}
	return ""
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

// scopeOrder is the words of a suggested name: the scope columns the name
// has, then each one it lacks, hard ones before context ones, then the other
// columns. A context scope the name lacks joins by its short name when it has
// one, else by its field, which takes a parameter.
func (r *resolver) scopeOrder(terms []term, missing []string) []string {
	var out []string
	for _, t := range terms {
		if slices.Contains(r.allScope, t.field) {
			out = append(out, r.displayTerm(t))
		}
	}
	for _, ctx := range []bool{false, true} {
		for _, f := range missing {
			switch c := r.ctxOf(f); {
			case (c != nil) != ctx:
			case c != nil && c.short != "":
				out = append(out, c.short)
			default:
				out = append(out, r.display(f))
			}
		}
	}
	for _, t := range terms {
		if !slices.Contains(r.allScope, t.field) {
			out = append(out, r.displayTerm(t))
		}
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
