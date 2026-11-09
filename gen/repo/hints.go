package genrepo

import (
	"fmt"
	"go/types"
	"slices"
	"strings"
)

// The fixes of a refusal: rename suggestions and hand-written bodies. A
// suggested rename must work: it derives, and the interface does not declare
// it already.

// badName explains properties that are no columns of the table: an
// operator the grammar leaves out, Or, a misspelled column, or a column a
// migration removed since the last run.
func (r *resolver) badName(fn *types.Func, rest string) *refusal {
	segs := splitAnd(rest)
	for bad, seg := range segs {
		if _, isCol := r.t.column(seg); isCol || r.short[seg] != "" {
			continue
		}
		if fail := r.operatorRefusal(fn, segs, bad); fail != nil {
			return fail
		}
		if a, b, ok := strings.Cut(seg, "Or"); ok && r.isColumn(a) && r.isColumn(b) {
			return &refusal{msg: "Or is not derived; only And is", body: bodyFor(fn, orCond(fn, segs, bad, a, b))}
		}
		names := r.nameWords()
		alt := "rename it with columns of " + r.t.sqlName + ": " + strings.Join(names, ", ")
		if s := suggest(seg, names); s != "" {
			if name := strings.Replace(fn.Name(), seg, s, 1); r.renameOK(fn, name) {
				alt = "rename it " + name + " (did you mean " + s + "?)"
			}
		}
		fail := &refusal{msg: fmt.Sprintf("no column %q on table %s", seg, r.t.sqlName), alt: alt}
		if r.oldColumn(seg) {
			// The schema moved, not the developer: the rename is the fix.
			fail.msg += " any more: the last generated base had it, so a migration removed or renamed it"
			fail.altFirst = true
		}
		return fail
	}
	return &refusal{msg: fmt.Sprintf("cannot split %q into columns of %s", rest, r.t.sqlName),
		alt: "join the properties with And"}
}

func (r *resolver) isColumn(field string) bool {
	_, ok := r.t.column(field)
	return ok || r.short[field] != ""
}

// nameWords are the words a derived name may use for the columns of the
// table: the Go fields, then the short names of its scope columns.
func (r *resolver) nameWords() []string {
	out := r.t.fields()
	for _, w := range r.words() {
		if w.text != w.field {
			out = append(out, w.text)
		}
	}
	return out
}

// operatorRefusal explains segment bad of segs when it is a column followed
// by an operator, with the hand-written body when the base has the
// predicate.
func (r *resolver) operatorRefusal(fn *types.Func, segs []string, bad int) *refusal {
	seg := segs[bad]
	for _, op := range unsupported {
		field, ok := strings.CutSuffix(seg, op)
		if !ok || !r.isColumn(field) {
			continue
		}
		fail := &refusal{msg: fmt.Sprintf("operator %s on %s is not derived; only equality (And) is", op, field),
			alt: "use the search builder"}
		col, _ := r.t.column(field)
		hint, variadic := opPredicate(op)
		if hint == "" || !hasPredicate(col, field+hint) {
			return fail
		}
		cond := condFor(fn, segs, func(i int, s, p string) string {
			if i == bad {
				if variadic {
					p += "..."
				}
				return "r.where." + field + hint + "(" + p + ")"
			}
			return "r.where." + s + "(" + p + ")"
		})
		fail.body = bodyFor(fn, cond)
		return fail
	}
	return nil
}

// opPredicate returns the suffix of the base predicate that does what op
// asks, and whether it takes a list.
func opPredicate(op string) (string, bool) {
	switch op {
	case "IgnoreCase":
		return "EqualsIgnoreCase", false
	case "Containing", "Like":
		return "Contains", false
	case "In":
		return "In", true
	case "After", "GreaterThan":
		return "After", false
	case "Before", "LessThan":
		return "Before", false
	default:
		return "", false
	}
}

func hasPredicate(c column, name string) bool {
	return slices.Contains(predicateNames(c), name)
}

// oldColumn reports whether the base on disk had a predicate on field: the
// column existed at the last run.
func (r *resolver) oldColumn(field string) bool {
	return r.old[r.t.whereName()+"."+field]
}

// taken reports whether the repository interface already declares name.
func (r *resolver) taken(fn *types.Func, name string) bool {
	if name == fn.Name() {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(r.b.iface, false, r.l.pkg.Types, name)
	return obj != nil
}

// renameOK reports whether fn, renamed, would derive and not collide with a
// method of the interface.
func (r *resolver) renameOK(fn *types.Func, name string) bool {
	if r.taken(fn, name) {
		return false
	}
	if r.nested {
		return true
	}
	r.nested = true
	defer func() { r.nested = false }()
	_, fail := r.derive(types.NewFunc(fn.Pos(), fn.Pkg(), name, signatureOf(fn)))
	return fail == nil
}

// renameHint proposes a derived name from the results and the parameter
// names: FindActive(ctx, productID string) ([]T, error) -> FindByProductID.
// fits is false when no derived method has the method's results.
func (r *resolver) renameHint(fn *types.Func) (hint string, fits bool) {
	sig := signatureOf(fn)
	res := sig.Results()
	var prefix string
	switch {
	case res.Len() == 1 && isError(res.At(0).Type()):
		prefix = "DeleteBy"
	case res.Len() == 2 && isError(res.At(1).Type()) && isBasic(res.At(0).Type(), types.Bool):
		prefix = "ExistsBy"
	case res.Len() == 2 && isError(res.At(1).Type()) && isBasic(res.At(0).Type(), types.Int64):
		prefix = "CountBy"
	case res.Len() == 2 && isError(res.At(1).Type()) && r.isDomainResult(res.At(0).Type()):
		prefix = "FindBy"
	default:
		return "", false
	}
	var cols []string
	for i := 1; i < sig.Params().Len(); i++ {
		col := columnNamed(sig.Params().At(i).Name(), r.t.fields())
		if col == "" {
			return fmt.Sprintf("rename it %s<Column>And<Column>… with one parameter per column, in name order "+
				"(columns of %s: %s)", prefix, r.t.sqlName, strings.Join(r.t.fields(), ", ")), true
		}
		cols = append(cols, col)
	}
	name := prefix + strings.Join(r.hintWords(cols), "And")
	if len(cols) == 0 && len(r.ctx) == 0 {
		name = map[string]string{"FindBy": "FindAll", "CountBy": "Count"}[prefix]
	}
	if name == "" || !r.renameOK(fn, name) {
		return "", true // the derivable name would not work here: write it by hand
	}
	return "rename it to a derivable name: " + name, true
}

// isDomainResult reports whether t is D, Option[D] or []D of the base's
// rows.
func (r *resolver) isDomainResult(t types.Type) bool {
	row, _ := rowOf(t)
	return types.Identical(row, r.t.domain)
}

// suggest returns the column the developer most likely meant, or "".
func suggest(seg string, fields []string) string {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }
	for _, c := range fields { // case or snake_case: ProductId, product_id
		if norm(c) == norm(seg) {
			return c
		}
	}
	for _, c := range fields { // a qualified column: TenantID -> PlatformTenantID
		if strings.HasSuffix(c, seg) {
			return c
		}
	}
	sw := words(seg)
	for _, c := range fields { // an abbreviation: OrgID -> OrganizationID
		if abbreviates(sw, words(c)) {
			return c
		}
	}
	best, bestD := "", 3
	for _, c := range fields {
		if dist := levenshtein(norm(seg), norm(c)); dist < bestD {
			best, bestD = c, dist
		}
	}
	if best != "" {
		return best
	}
	var shared []string // a renamed column keeps its first word: ExternalID -> ExternalRef
	for _, c := range fields {
		if cw := words(c); len(sw) > 1 && len(cw) > 1 && cw[0] == sw[0] {
			shared = append(shared, c)
		}
	}
	if len(shared) == 1 {
		return shared[0]
	}
	return ""
}

// abbreviates reports whether each word of short starts the word of long at
// its place.
func abbreviates(short, long []string) bool {
	if len(short) != len(long) {
		return false
	}
	for i := range long {
		if !strings.HasPrefix(strings.ToLower(long[i]), strings.ToLower(short[i])) {
			return false
		}
	}
	return true
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// condFor renders the r.where predicates of a name's segments, one
// parameter each; render gives the predicate of segment i.
func condFor(fn *types.Func, segs []string, render func(i int, seg, param string) string) string {
	params := signatureOf(fn).Params()
	parts := make([]string, 0, len(segs))
	for i, seg := range segs {
		p := "…"
		if i+1 < params.Len() {
			p = nameOr(params.At(i+1).Name(), i)
		}
		parts = append(parts, render(i, seg, p))
	}
	return andAll(parts)
}

// andAll joins conditions with AND: a.AND(b).AND(c), or nil for none.
func andAll(parts []string) string {
	if len(parts) == 0 {
		return "nil"
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		b.WriteString(".AND(" + p + ")")
	}
	return b.String()
}

// orCond renders the condition of segs where segment or is "AOrB": one
// parameter per column, the Or pair taking two.
func orCond(fn *types.Func, segs []string, or int, a, b string) string {
	params := signatureOf(fn).Params()
	next := 1
	arg := func() string {
		p := "…"
		if next < params.Len() {
			p = nameOr(params.At(next).Name(), next-1)
		}
		next++
		return p
	}
	parts := make([]string, len(segs))
	for i, seg := range segs {
		if i == or {
			parts[i] = "r.where." + a + "(" + arg() + ")"
			parts[i] += ".OR(r.where." + b + "(" + arg() + "))"
		} else {
			parts[i] = "r.where." + seg + "(" + arg() + ")"
		}
	}
	return andAll(parts)
}

// bodyFor renders a hand-written body for fn with the condition cond on the
// base, and an order for a find.
func bodyFor(fn *types.Func, cond string, order ...string) string {
	res := signatureOf(fn).Results()
	ord := ""
	if len(order) > 0 {
		ord = ", " + strings.Join(order, ", ")
	}
	isDelete := strings.HasPrefix(fn.Name(), "DeleteBy")
	switch {
	case res.Len() == 1:
		return "_, err := r.deleteWhere(ctx, " + cond + ")" + bodyBreak + "return err"
	case res.Len() != 2:
		return ""
	case isNamed(res.At(0).Type(), functionalPath, "Option") && ord != "":
		return "return r.first(ctx, " + cond + ord + ")"
	case isNamed(res.At(0).Type(), functionalPath, "Option"):
		return "return r.findOne(ctx, " + cond + ")"
	case isSlice(res.At(0).Type()):
		return "return r.findAll(ctx, " + cond + ord + ")"
	case isBasic(res.At(0).Type(), types.Bool) && isDelete:
		return "n, err := r.deleteWhere(ctx, " + cond + ")" + bodyBreak + "return n > 0, err"
	case isBasic(res.At(0).Type(), types.Bool):
		return "return r.exists(ctx, " + cond + ")"
	case isBasic(res.At(0).Type(), types.Int64) && isDelete:
		return "return r.deleteWhere(ctx, " + cond + ")"
	case isBasic(res.At(0).Type(), types.Int64):
		return "return r.count(ctx, " + cond + ")"
	}
	return ""
}

// The indentation of a fix: code lines, body lines and alternatives line up
// under "  fix: ".
const (
	codeIndent = "\n         "
	bodyBreak  = "\n             "
	orIndent   = "\n       or "
)

// hintWords are the words of a suggested name for the columns cols, taken
// from the parameters: a scope column by its short name, and each context
// scope that cols lack, by its short name, after the leading scope columns.
func (r *resolver) hintWords(cols []string) []string {
	var words []string
	for _, c := range cols {
		if r.ctxOf(c) != nil {
			words = append(words, c) // a parameter names it: the field form
		} else {
			words = append(words, r.display(c))
		}
	}
	at := 0
	for at < len(cols) && slices.Contains(r.scope, cols[at]) && r.ctxOf(cols[at]) == nil {
		at++
	}
	for _, c := range r.missingCtx(termsOfFields(cols)) {
		if c.short == "" {
			continue
		}
		words = slices.Insert(words, at, c.short)
		at++
	}
	return words
}

// termsOfFields reads fields as terms that bind a parameter each.
func termsOfFields(fields []string) []term {
	out := make([]term, len(fields))
	for i, f := range fields {
		out[i] = term{field: f}
	}
	return out
}
