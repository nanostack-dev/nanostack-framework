package genrepo

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// checkParams binds the value parameters to the properties by position
// (design D22): ctx first, one parameter per property, of the column's model
// type or a named type over it, never a pointer, and never named after
// another column of the name.
func (r *resolver) checkParams(fn *types.Func, m *derivedMethod) *refusal {
	if m.op.isWrite() {
		return r.checkWriteParams(fn, m)
	}
	params := signatureOf(fn).Params()
	if params.Len() == 0 || !isNamed(params.At(0).Type(), "context", "Context") {
		return &refusal{msg: "the first parameter is not context.Context",
			alt: "take ctx context.Context as the first parameter"}
	}
	if got, want := params.Len()-1, len(m.preds); got != want {
		alt := "take no parameter after ctx"
		if want > 0 {
			alt = "take one parameter per property, in name order: " + strings.Join(m.preds, ", ")
		}
		return &refusal{msg: fmt.Sprintf("has %d value parameter(s), the name has %d propert%s", got, want,
			plural(want)), alt: alt}
	}
	used := map[string]bool{"ctx": true, "r": true, "err": true, "found": true, "row": true, "ok": true,
		"n": true}
	for _, pkg := range importedNames(fn) {
		used[pkg] = true
	}
	for i, pred := range m.preds {
		p, fail := r.checkParam(params.At(i+1), i, pred, m.preds)
		if fail != nil {
			return fail
		}
		for used[p.name] {
			p.name += "Arg"
		}
		used[p.name] = true
		m.params = append(m.params, p)
	}
	return nil
}

// checkParam checks parameter v, at index i among the value parameters,
// bound to the property pred.
func (r *resolver) checkParam(v *types.Var, i int, pred string, preds []string) (param, *refusal) {
	name := nameOr(v.Name(), i)
	col, _ := r.t.column(pred)
	if _, isPtr := types.Unalias(v.Type()).(*types.Pointer); isPtr {
		return param{}, &refusal{
			msg: fmt.Sprintf("parameter %s is %s; a derived predicate compares with =, which never matches NULL",
				name, r.qualified(v.Type())),
			alt: "take a value type (for NULL, the hand-written body uses r.where." + pred + "IsNull())"}
	}
	if named := columnNamed(v.Name(), r.t.fields()); named != "" && named != pred {
		snakes := make([]string, len(preds))
		for j, f := range preds {
			snakes[j] = r.sqlName(f)
		}
		return param{}, &refusal{altFirst: true,
			msg: fmt.Sprintf("parameter %s is bound to %s by its position, but its name says %s; "+
				"the name orders the columns %s", name, col.sqlName, r.sqlName(named), strings.Join(snakes, ", ")),
			alt: "reorder the parameters to follow the name, or rename the method to follow the parameters"}
	}
	if col.lit == litNone {
		why := "its model type is " + r.qualified(col.value)
		if col.notText {
			why = "its catalogue type is " + col.dbType + ", which does not compare with a text value"
		}
		return param{}, &refusal{msg: fmt.Sprintf("column %s.%s has no equality predicate on the base: %s",
			r.t.sqlName, col.sqlName, why)}
	}
	if named, isNamed := types.Unalias(v.Type()).(*types.Named); isNamed {
		// A named type that spells another column of the table, such as ProductID for org_id, is a mix-up
		// that the conversion would hide.
		if other := columnNamed(named.Obj().Name(), r.t.fields()); other != "" && other != pred {
			return param{}, &refusal{
				msg: fmt.Sprintf("parameter %s has type %s, which says column %s, but it is bound to %s",
					name, r.qualified(v.Type()), r.sqlName(other), col.sqlName),
				alt: fmt.Sprintf("pass the value of %s, or a named type that does not say another column", col.sqlName)}
		}
	}
	conv, ok := convertible(v.Type(), col.value)
	if !ok {
		return param{}, &refusal{
			msg: fmt.Sprintf("parameter %s has type %s; column %s.%s is %s", name, r.qualified(v.Type()),
				r.t.sqlName, col.sqlName, r.qualified(col.value)),
			alt: fmt.Sprintf("declare %s as %s (or a named type over it)", name, r.qualified(col.value))}
	}
	p := param{name: name, typ: v.Type()}
	if conv {
		p.conv = col.value
	}
	return p, nil
}

// convertible reports whether a parameter of type p passes to a predicate
// on a column of model type v: the same type, or a named type over it,
// which conv says to convert.
func convertible(p, v types.Type) (conv, ok bool) {
	if types.Identical(p, v) {
		return false, true
	}
	if _, named := types.Unalias(p).(*types.Named); named && types.Identical(p.Underlying(), v.Underlying()) {
		return true, true
	}
	return false, false
}

// checkResults checks the results against the return shapes (design D23).
func (r *resolver) checkResults(fn *types.Func, m *derivedMethod) *refusal {
	results := signatureOf(fn).Results()
	last := results.Len() - 1
	if last < 0 || !isError(results.At(last).Type()) {
		return &refusal{msg: "the last result is not error", alt: "return error as the last result"}
	}
	wrong := func(alt string) *refusal {
		return &refusal{msg: fmt.Sprintf("a derived %s cannot return %s", m.op, r.qualified(results)), alt: alt}
	}
	first := func(k types.BasicKind) bool { return results.Len() == 2 && isBasic(results.At(0).Type(), k) }
	switch m.op {
	case opCreate, opUpdate, opUpsert:
		return r.writeShape(results, m, wrong)
	case opFind:
		return r.findShape(results, m, wrong)
	case opExists:
		if first(types.Bool) {
			m.shape = shapeBool
			return nil
		}
		return wrong("return (bool, error)")
	case opCount:
		if first(types.Int64) {
			m.shape = shapeInt64
			return nil
		}
		if name, ok := strings.CutPrefix(fn.Name(), "CountBy"); ok && first(types.Bool) &&
			r.renameOK(fn, "ExistsBy"+name) {
			return wrong("rename it ExistsBy" + name + " for a yes or no answer, or return (int64, error)")
		}
		return wrong("return (int64, error)")
	case opDelete:
	}
	switch {
	case results.Len() == 1:
		m.shape = shapeErr
	case first(types.Bool):
		m.shape = shapeDeleted
	case first(types.Int64):
		m.shape = shapeAffected
	default:
		return wrong(`return error, (bool, error) for "a row was deleted", or (int64, error) for the row count`)
	}
	return nil
}

// findShape checks the results of a find: Option[D], []D or D, and error.
func (r *resolver) findShape(results *types.Tuple, m *derivedMethod, wrong func(string) *refusal) *refusal {
	dom := r.qualified(r.t.domain)
	if results.Len() != 2 {
		return wrong(fmt.Sprintf("return (functional.Option[%s], error), ([]%s, error) or (%s, error)", dom, dom, dom))
	}
	elem, s := rowOf(results.At(0).Type())
	if len(m.preds) == 0 && s != shapeSlice {
		return wrong(fmt.Sprintf("return ([]%s, error)", dom))
	}
	m.shape = s
	return r.sameDomain(elem)
}

// rowOf returns the row type of a find result and its shape: T of
// Option[T], T of []T, or the type itself.
func rowOf(t types.Type) (types.Type, shape) {
	if n, ok := types.Unalias(t).(*types.Named); ok && isNamed(n, functionalPath, "Option") {
		return n.TypeArgs().At(0), shapeOption
	}
	if s, ok := t.(*types.Slice); ok {
		return s.Elem(), shapeSlice
	}
	return t, shapeOne
}

// sameDomain checks the row type a find returns against the base's rows.
func (r *resolver) sameDomain(res types.Type) *refusal {
	if types.Identical(res, r.t.domain) {
		return nil
	}
	from := "the mapper returns"
	if r.t.mapper == nil {
		from = "the base returns"
	}
	return &refusal{
		msg: fmt.Sprintf("returns %s, but %s %s", r.qualified(res), from, r.qualified(r.t.domain)),
		alt: fmt.Sprintf("return %s, or bind the mapper that returns %s in nanorepo.Implement",
			r.qualified(r.t.domain), r.qualified(res))}
}

// checkUnique proves a single-row find (design D24): its properties cover
// every column of the primary key or of a key UniqueKeys lists. No silent
// first row.
func (r *resolver) checkUnique(fn *types.Func, m *derivedMethod) *refusal {
	if m.op.isWrite() {
		return r.checkWrite(m)
	}
	if m.op != opFind || (m.shape != shapeOne && m.shape != shapeOption) {
		return nil
	}
	cols := make([]string, len(m.preds))
	for i, p := range m.preds {
		cols[i] = r.sqlName(p)
	}
	if !r.t.keysFound {
		return &refusal{altFirst: true,
			msg: fmt.Sprintf("it returns one row, and the table package has no UniqueKeys for %s to prove it",
				r.t.sqlName),
			alt: "run nanostack gen db, which writes UniqueKeys into the table package, then rerun nanostack gen repo"}
	}
	if k, ok := r.t.provingKey(m.preds); ok {
		m.key = &k
		return nil
	}
	fail := r.keyRefusal(fn, m, cols)
	named := func(f string) bool { return slices.ContainsFunc(m.terms, func(t term) bool { return t.field == f }) }
	// A body that leaves out a scope column that the name only implies would read across it: no body.
	if m.shape == shapeOption && !slices.ContainsFunc(r.allScope, func(f string) bool { return !named(f) }) {
		// By hand, the row is chosen by an order, never by chance.
		// It ends on every column of the primary key: one column of a composite key is not unique.
		var pk []string
		for _, c := range r.t.columns {
			if c.primary {
				pk = append(pk, "r.t."+c.field+".ASC()")
			}
		}
		order := "r.t.<Column>.ASC()"
		if len(pk) > 0 {
			order = strings.Join(pk, ", ")
			if _, ok := r.t.column("CreatedAt"); ok {
				order = "r.t.CreatedAt.ASC(), " + order
			}
		}
		fail.body = bodyFor(fn, r.condOf(fn, m.terms), order)
	}
	return fail
}

// keyRefusal is the refusal of a single-row find that no key proves. The
// fix names what the keys of the table ask for: a context scope in its short
// form binds no parameter, so it is not part of the columns that the name
// compares, even though it filters; a key that holds more columns than the name
// is completed by naming them; only when no key is close is a new unique index
// suggested, on every column that the name filters on.
func (r *resolver) keyRefusal(fn *types.Func, m *derivedMethod, cols []string) *refusal {
	dom := r.qualified(r.t.domain)
	fail := &refusal{
		altFirst: true,
		msg: fmt.Sprintf(
			"it returns one row, but (%s) covers no unique key; %s",
			strings.Join(cols, ", "),
			r.t.keyList(),
		),
	}
	// The columns that the name filters on, a context scope by its short name included.
	var all, shortCtx []string
	for _, t := range m.terms {
		all = append(all, t.field)
		if t.ctx != nil {
			shortCtx = append(shortCtx, t.field)
		}
	}
	if k, ok := r.t.provingKey(all); ok && len(shortCtx) > 0 {
		return r.shortContextRefusal(fn, m, fail, k, shortCtx)
	}
	var compared []string // the columns the name compares, which a key must hold: the tenant columns only narrow
	for _, f := range all {
		if !slices.Contains(r.allScope, f) {
			compared = append(compared, f)
		}
	}
	if k, missing, ok := r.completingKey(all, compared); ok {
		names := make([]string, len(missing))
		for i, f := range missing {
			names[i] = r.display(f)
			if r.ctxOf(f) != nil {
				names[i] = f // the short name of a context scope binds no parameter, so it proves nothing
			}
		}
		fail.alt = fmt.Sprintf("name %s too, which completes %s (%s), or return []%s", strings.Join(names, " and "),
			k.name, strings.Join(k.sqlCols, ", "), dom)
		return fail
	}
	fail.alt = fmt.Sprintf("return []%s; or add a unique index on (%s) in a migration and rerun nanostack gen db",
		dom, strings.Join(r.columnsOf(all), ", "))
	if len(r.t.notKeys) > 0 {
		fail.alt += " (a partial or expression unique index above is not a key: if one covers these columns, " +
			"drop its WHERE or its expression)"
	}
	if len(shortCtx) > 0 {
		fail.alt += ", and name " + strings.Join(r.columnsOf(shortCtx), " and ") +
			" by its Go field, which takes a parameter, so that the key covers it"
	}
	return fail
}

// shortContextRefusal is keyRefusal where the key holds a context scope that
// the name gives by its short name.
func (r *resolver) shortContextRefusal(fn *types.Func, m *derivedMethod, fail *refusal, k uniqueKey,
	shortCtx []string) *refusal {
	var held []string
	for _, f := range shortCtx {
		if slices.Contains(k.fields, f) {
			held = append(held, r.sqlName(f))
		}
	}
	fail.msg += fmt.Sprintf("; %s holds %s, which the short name binds to no parameter: the registered function "+
		"may keep every workspace, so it does not prove one row", k.name, strings.Join(held, " and "))
	fail.alt = fmt.Sprintf("rename it %s, which takes the value of %s as a parameter, or return []%s",
		r.fieldName(fn, m, shortCtx), strings.Join(held, " and "), r.qualified(r.t.domain))
	return fail
}

// completingKey returns the key that holds every one of compared and needs
// the fewest columns more than fields, and those columns.
func (r *resolver) completingKey(fields, compared []string) (uniqueKey, []string, bool) {
	var best uniqueKey
	var bestMissing []string
	found := false
	for _, k := range r.t.keys {
		if slices.Contains(k.fields, "") || len(compared) == 0 ||
			slices.ContainsFunc(compared, func(f string) bool { return !slices.Contains(k.fields, f) }) {
			continue
		}
		var missing []string
		for _, f := range k.fields {
			if !slices.Contains(fields, f) {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 && (!found || len(missing) < len(bestMissing)) {
			best, bestMissing, found = k, missing, true
		}
	}
	return best, bestMissing, found
}

func (r *resolver) columnsOf(fields []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = r.sqlName(f)
	}
	return out
}

// fieldName is fn's name with each short context term of fields spelled as
// its Go field.
func (r *resolver) fieldName(fn *types.Func, m *derivedMethod, fields []string) string {
	words := make([]string, len(m.terms))
	for i, t := range m.terms {
		words[i] = r.displayTerm(t)
		if t.ctx != nil && slices.Contains(fields, t.field) {
			words[i] = t.field
		}
	}
	prefix := suggestNamed(fn.Name(), nil)
	if m.internal {
		return prefix + strings.Join(words, "And") + "Internal"
	}
	return prefix + strings.Join(words, "And")
}

// methodSQL is the statement a derived method runs, for its comment and
// -explain.
func (r *resolver) methodSQL(m *derivedMethod) string {
	if m.op.isWrite() {
		return r.writeSQL(m)
	}
	where := ""
	if len(m.terms) > 0 {
		parts := make([]string, len(m.terms))
		next := 0
		for i, t := range m.terms {
			if t.ctx != nil {
				parts[i] = funcWord(t.ctx.fn) + "(ctx, " + t.ctx.col + ")"
				continue
			}
			next++
			parts[i] = r.sqlName(t.field) + " = $" + strconv.Itoa(next)
		}
		where = " WHERE " + strings.Join(parts, " AND ")
	}
	switch m.op {
	case opExists:
		return "SELECT COUNT(*) FROM (SELECT 1 FROM " + r.t.sqlName + where + " LIMIT 1) found"
	case opCount:
		return "SELECT COUNT(*) FROM " + r.t.sqlName + where
	case opDelete:
		return "DELETE FROM " + r.t.sqlName + where
	case opFind, opCreate, opUpdate, opUpsert:
	}
	s := "SELECT * FROM " + r.t.sqlName + where
	if m.key != nil {
		s += " LIMIT 2 -- at most one row: " + m.key.String() + "; a second row is ErrMoreThanOneRow"
	}
	return s
}

// sqlName returns the column name of a Go field of the table.
func (r *resolver) sqlName(field string) string {
	return columnOf(r.t, field)
}
