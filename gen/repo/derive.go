package genrepo

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// op is what a derived method does with the rows its name selects.
type op int

const (
	opFind op = iota
	opExists
	opCount
	opDelete
	opCreate // Create or CreateIn<Scope...>: INSERT ... RETURNING
	opUpdate // UpdateBy<Scope...>And<Key...>: UPDATE ... RETURNING
	opUpsert // UpsertBy<Scope...>And<Key...>: INSERT ... ON CONFLICT ... DO UPDATE
)

func (o op) String() string {
	return [...]string{"find", "exists", "count", "delete", "create", "update", "upsert"}[o]
}

// isWrite reports whether o writes a value: Create, UpdateBy or UpsertBy.
func (o op) isWrite() bool { return o >= opCreate }

// changesRows reports whether o changes rows: a write or a delete.
func (o op) changesRows() bool { return o == opDelete || o.isWrite() }

// shape is the result list of a derived method (design D23).
type shape int

const (
	shapeOption   shape = iota // (functional.Option[D], error)
	shapeSlice                 // ([]D, error)
	shapeOne                   // (D, error): sql.ErrNoRows on no row
	shapeBool                  // (bool, error) of an exists
	shapeInt64                 // (int64, error) of a count
	shapeErr                   // error of a delete
	shapeDeleted               // (bool, error) of a delete: a row was deleted
	shapeAffected              // (int64, error) of a delete: the rows deleted
)

// prefixes are the derived name prefixes followed by properties.
var prefixes = []struct { //nolint:gochecknoglobals // read-only table.
	name string
	op   op
}{
	{"FindBy", opFind}, {"GetBy", opFind}, {"ExistsBy", opExists}, {"CountBy", opCount}, {"DeleteBy", opDelete},
	{"CreateIn", opCreate}, {"UpdateBy", opUpdate}, {"UpsertBy", opUpsert},
}

const grammarMsg = "it does not start with FindBy, GetBy, ExistsBy, CountBy, DeleteBy, CreateIn, UpdateBy, " +
	"UpsertBy, FindAll, Count or Create"

// unsupported are operators a name may try after a column; only equality,
// joined by And, derives (design D21). Longer operators come first.
var unsupported = []string{ //nolint:gochecknoglobals // read-only table.
	"IgnoreCase", "NotIn", "In", "NotLike", "Like", "Containing", "StartingWith", "EndingWith",
	"GreaterThanEqual", "GreaterThan", "LessThanEqual", "LessThan", "Before", "After", "Between",
	"IsNotNull", "IsNull", "True", "False", "Not",
}

// derivedMethod is an interface method whose name derives: its body is one
// call to a helper of the generated base.
type derivedMethod struct {
	fn       *types.Func
	op       op
	shape    shape
	internal bool       // the name ends in Internal
	unscoped bool       // Internal skipped the tenant scope (a read)
	skipped  []string   // the scope fields an unscoped method does not filter on
	cross    []string   // the context scope columns the name gives by their Go field: the value is a parameter, not ctx
	preds    []string   // Go fields of the properties that bind a parameter, in name order
	terms    []term     // every property, in name order: preds and the context scopes named by short name
	params   []param    // one per entry of preds; a write adds its value last
	key      *uniqueKey // the key that proves a single-row result, or the conflict key of an upsert
	write    *writePlan // the columns of a write; nil for a read or a delete
}

// param is one value parameter of a derived method.
type param struct {
	name string
	typ  types.Type
	conv types.Type // non-nil: the parameter is a named type over the column type, converted to it
}

// refusal says why a name does not derive, and what else would work.
type refusal struct {
	msg      string
	alt      string // the other fix, read after "or": a rename, a signature change
	body     string // a hand-written body, when the reason suggests one
	grammar  bool   // the name is outside the grammar: not a derived-looking name
	altFirst bool   // the alternative is the better fix, so it comes first
}

// derive decides whether fn derives on the table of the binding. It reports
// nothing: a hand-written method of the same name wins, so the caller
// decides what a refusal means.
func (r *resolver) derive(fn *types.Func) (*derivedMethod, *refusal) {
	if r.t.view {
		return nil, &refusal{grammar: false,
			msg: "table " + r.t.sqlName + " is a view: nanostack gen db types every view column as nullable, so " +
				"no tenant column of a view can be trusted as a scope, and a derived delete on an updatable view " +
				"would reach the rows of every tenant",
			alt: "write it by hand, with the tenant filter in its condition, or bind the base table of the view"}
	}
	m, fail := r.parseName(fn)
	if fail != nil {
		return nil, fail
	}
	if fail = r.checkParams(fn, m); fail != nil {
		return nil, fail
	}
	if fail = r.checkResults(fn, m); fail != nil {
		return nil, fail
	}
	if fail = r.checkUnique(fn, m); fail != nil {
		return nil, fail
	}
	return m, nil
}

// parseName reads the grammar of the name: prefix, properties, Internal,
// and the tenant scope rule.
func (r *resolver) parseName(fn *types.Func) (*derivedMethod, *refusal) {
	m := &derivedMethod{fn: fn}
	base := fn.Name()
	if trimmed, ok := strings.CutSuffix(base, "Internal"); ok && trimmed != "" && !r.endsInColumn(base) {
		m.internal, base = true, trimmed
	}
	rest, known := parsePrefix(base, m)
	if !known {
		return nil, r.grammarRefusal(fn)
	}
	if m.internal && m.op.isWrite() {
		return nil, &refusal{msg: "a derived write never ends in Internal: it skips no scope",
			alt: "rename it " + strings.TrimSuffix(fn.Name(), "Internal") + ", or write it by hand"}
	}
	if rest == "" && base != "FindAll" && base != "Count" && base != "Create" {
		return nil, &refusal{msg: "no property after By",
			alt: "name at least one column after By, or use FindAll or Count"}
	}
	if rest != "" {
		toks, ok := splitTerms(rest, r.words())
		// A name that splits into columns as it is, such as one with a column
		// order_by, is no OrderBy operator.
		if before, after, found := strings.Cut(rest, "OrderBy"); found && !ok {
			return nil, r.orderByRefusal(fn, before, after)
		}
		if !ok {
			return nil, r.badName(fn, rest)
		}
		m.terms = r.termsOf(toks)
		r.collectPreds(m)
	}
	// A table that no derived write can scope (a nullable scope column, no hard
	// scope) gets the write refusal, which says so, before the read rule.
	if !r.writeUnscopable(m) {
		if fail := r.scopeRules(fn, m, base); fail != nil {
			return nil, fail
		}
	}
	if fail := r.writeScopeRule(m, base); fail != nil {
		return nil, fail
	}
	return m, nil
}

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

// scopeRules applies the scope rules to the terms of m: the name gives every
// scope column of the table, hard or context, nullable or not (a column it
// names stands for the ones it implies, Postgres.ScopeImplies), and the scope
// words come first. Internal skips the missing columns on a read, which marks
// m unscoped; it returns the refusal otherwise.
func (r *resolver) scopeRules(fn *types.Func, m *derivedMethod, base string) *refusal {
	missing := r.missingFor(m)
	misordered := r.misordered(m.terms)
	if len(missing) == 0 && !misordered {
		return nil
	}
	if m.internal && m.op != opDelete && !m.op.isWrite() {
		if len(missing) > 0 {
			m.unscoped, m.skipped = true, missing
		}
		return nil
	}
	return r.scopeRefusal(fn, m, base, missing)
}

// endsInColumn reports whether the properties of name split into columns
// as they are: its last column ends in Internal, such as IsInternal, so the
// suffix names the column, not the scope rule.
func (r *resolver) endsInColumn(name string) bool {
	rest, known := parsePrefix(name, &derivedMethod{})
	if !known || rest == "" {
		return false
	}
	_, ok := splitTerms(rest, r.words())
	return ok
}

// parsePrefix sets the operation of m from the prefix of base, and returns
// the properties after it. known is false outside the grammar.
func parsePrefix(base string, m *derivedMethod) (rest string, known bool) {
	switch base {
	case "Create":
		m.op = opCreate
		return "", true
	case "FindAll":
		m.op = opFind
		return "", true
	case "Count":
		m.op = opCount
		return "", true
	}
	for _, p := range prefixes {
		if after, ok := strings.CutPrefix(base, p.name); ok {
			m.op = p.op
			return after, true
		}
	}
	return "", false
}

// grammarRefusal is the refusal of a name outside the grammar, with a
// derivable rename when the parameters and the results suggest one.
func (r *resolver) grammarRefusal(fn *types.Func) *refusal {
	hint, fits := r.renameHint(fn)
	fail := &refusal{grammar: true, msg: grammarMsg, alt: hint}
	if !fits {
		fail.msg += ", and no derived method returns " + r.qualified(signatureOf(fn).Results())
	}
	return fail
}

// orderByRefusal is the refusal of FindByXOrderByY, with the body that
// orders by hand.
func (r *resolver) orderByRefusal(fn *types.Func, before, after string) *refusal {
	col, dir := strings.CutSuffix(after, "Desc")
	order := ".DESC()"
	if !dir {
		col, _ = strings.CutSuffix(after, "Asc")
		order = ".ASC()"
	}
	fail := &refusal{msg: "OrderBy is not derived", alt: "use the search builder for a client-chosen order"}
	if _, isCol := r.t.column(col); isCol {
		// The properties before OrderBy, as terms: a short name of a scope column is its field, and a
		// context scope is its function, which binds no parameter.
		cond := ""
		if toks, ok := splitTerms(before, r.words()); ok {
			cond = r.condOf(fn, r.termsOf(toks))
		} else {
			cond = r.condSegs(fn, splitAnd(before), -1, nil)
		}
		fail.body = bodyFor(fn, cond, "r.t."+col+order)
	}
	return fail
}

// scopeRefusal is the refusal of a name on a scoped table that does not name
// every scope column (design D25, ADR 0026), or names one after a column that
// is no scope column. Internal skips the scope on reads only: a scope-free
// write is hand-written, where review sees it.
func (r *resolver) scopeRefusal(fn *types.Func, m *derivedMethod, base string, missing []string) *refusal {
	scoped := suggestNamed(base, r.scopeOrder(m.terms, missing))
	cols := make([]string, len(r.allScope))
	for i, f := range r.allScope {
		cols[i] = r.sqlName(f)
	}
	what := fmt.Sprintf("table %s is scoped by %s (Postgres.Scopes), and ", r.t.sqlName, strings.Join(cols, ", "))
	var words, newParams []string
	for _, f := range missing {
		c := r.ctxOf(f)
		switch {
		case c != nil && c.short != "":
			words = append(words, c.short)
		default:
			words = append(words, r.display(f))
			newParams = append(newParams, lowerFirst(f))
		}
	}
	var fix string
	switch {
	case len(missing) == 0:
		what += "the name has a column before a scope column: the scope words come first"
		fix = "move the scope words to the front of the name and of the parameters: " + scoped
	case r.taken(fn, scoped):
		what += "the name does not name " + strings.Join(r.columnsOf(missing), " or ")
		fix = "name " + strings.Join(words, " and ") + " first (" + scoped + " is already declared)"
	default:
		what += "the name does not name " + strings.Join(r.columnsOf(missing), " or ")
		fix = "rename it " + scoped
		if len(newParams) > 0 {
			fix += ", with " + strings.Join(newParams, " and ") + " as parameter, scope words first"
		}
	}
	for _, f := range missing {
		switch c := r.ctxOf(f); {
		case c == nil:
		case c.short != "":
			fix += "; " + c.short + " binds no parameter: it applies " + funcWord(c.fn) + " to " + c.col
		default:
			fix += "; " + c.field + " takes a parameter, and a short name from Postgres.ScopeNames binds none"
		}
	}
	hardMissing := slices.ContainsFunc(missing, func(f string) bool { return r.ctxOf(f) == nil })
	namesScope := slices.ContainsFunc(m.terms, func(t term) bool { return slices.Contains(r.allScope, t.field) })
	if hardMissing && namesScope && !m.op.isWrite() {
		fix += "; if a column the name has determines the others in the schema, declare it in Postgres.ScopeImplies"
	}
	if m.op == opDelete && m.internal {
		return &refusal{
			msg:  what + ": Internal skips the scope on derived reads only",
			alt:  fix + "; a delete across tenants is written by hand, where review sees it",
			body: bodyFor(fn, r.condOf(fn, m.terms)),
		}
	}
	switch {
	case m.op == opDelete:
		return &refusal{msg: what, alt: fix}
	case m.op == opCreate:
		return &refusal{msg: what + ": a derived Create takes its scope from the name, never from the value", alt: fix}
	case m.op == opUpsert:
		return &refusal{msg: what + ": a conflict key without the scope could reach the row of another tenant",
			alt: fix}
	case m.op.isWrite():
		return &refusal{msg: what, alt: fix}
	}
	return &refusal{msg: what, alt: fix + ", or end the name in Internal for a trusted system path"}
}

// splitTerms resolves "ProductIDAndExternalID" against the tokens of the
// columns: whole Go field names and the short names of scope columns, longest
// first, so a column called BrandAndModel still parses and ProductID beats a
// short name Product.
func splitTerms(s string, toks []word) ([]word, bool) {
	byLen := slices.Clone(toks)
	slices.SortStableFunc(byLen, func(a, b word) int { return len(b.text) - len(a.text) })
	var walk func(string) ([]word, bool)
	walk = func(s string) ([]word, bool) {
		for _, c := range byLen {
			tail, ok := strings.CutPrefix(s, c.text)
			if !ok {
				continue
			}
			if tail == "" {
				return []word{c}, true
			}
			if next, isAnd := strings.CutPrefix(tail, "And"); isAnd {
				if more, found := walk(next); found {
					return append([]word{c}, more...), true
				}
			}
		}
		return nil, false
	}
	return walk(s)
}

// splitPreds resolves "ProductIDAndExternalID" against the Go fields of the
// columns, longest first.
func splitPreds(s string, fields []string) ([]string, bool) {
	toks := make([]word, len(fields))
	for i, f := range fields {
		toks[i] = word{f, f}
	}
	got, ok := splitTerms(s, toks)
	if !ok {
		return nil, false
	}
	out := make([]string, len(got))
	for i, t := range got {
		out[i] = t.field
	}
	return out, true
}

// splitAnd splits a name's properties at each And before an upper-case
// letter.
func splitAnd(s string) []string {
	var out []string
	start := 0
	for i := 1; i+3 < len(s); i++ {
		if s[i:i+3] == "And" && s[i+3] >= 'A' && s[i+3] <= 'Z' {
			out = append(out, s[start:i])
			start = i + 3
		}
	}
	return append(out, s[start:])
}

// columnNamed returns the Go field a parameter name spells (productID ->
// ProductID, id -> ID), or "" when the name is no column (tenantID).
func columnNamed(name string, fields []string) string {
	if name == "" {
		return ""
	}
	for _, f := range fields {
		if strings.EqualFold(f, name) || strings.EqualFold(f, strings.TrimSuffix(name, "Id")+"ID") {
			return f
		}
	}
	return ""
}

func signatureOf(fn *types.Func) *types.Signature {
	sig, _ := fn.Type().(*types.Signature)
	return sig
}

// importedNames returns the names of the packages of the types in fn's
// signature: a parameter named after one would hide the package in the
// generated body.
func importedNames(fn *types.Func) []string {
	var out []string
	var walk func(t types.Type)
	walk = func(t types.Type) {
		switch x := types.Unalias(t).(type) {
		case *types.Named:
			if x.Obj().Pkg() != nil {
				out = append(out, x.Obj().Pkg().Name())
			}
			for a := range x.TypeArgs().Types() {
				walk(a)
			}
		case *types.Pointer:
			walk(x.Elem())
		case *types.Slice:
			walk(x.Elem())
		case *types.Map:
			walk(x.Key())
			walk(x.Elem())
		}
	}
	sig := signatureOf(fn)
	for v := range sig.Params().Variables() {
		walk(v.Type())
	}
	for v := range sig.Results().Variables() {
		walk(v.Type())
	}
	return append(out, "sql", "functional", "context")
}

func isError(t types.Type) bool { return types.Identical(t, types.Universe.Lookup("error").Type()) }

func isNamed(t types.Type, path, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path && n.Obj().Name() == name
}

func isSlice(t types.Type) bool {
	_, ok := t.(*types.Slice)
	return ok
}

func isBasic(t types.Type, k types.BasicKind) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == k
}

func nameOr(name string, i int) string {
	if name == "" || name == "_" {
		return "p" + strconv.Itoa(i+1)
	}
	return name
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
