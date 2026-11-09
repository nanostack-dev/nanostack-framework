package genrepo

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// writePlan is what a derived write sets, beyond its name: the columns it
// leaves to the database, the context scopes it stamps, the columns it
// updates, and the scope columns an upsert keeps (ADR 0026, derived writes).
type writePlan struct {
	dbFields []string   // Go fields of Postgres.DatabaseColumns on the table: never written
	stamps   []ctxStamp // context scope columns set from their Stamp function (Create, Upsert)
	set      []string   // Go fields that UPDATE or ON CONFLICT DO UPDATE sets
	target   []string   // the conflict columns of an upsert, in key order
	guards   []string   // scope fields an upsert updates only when the existing row has the new value
}

// ctxStamp is a context scope column that a Create or an Upsert sets from the
// function that ContextScope.Stamp registers.
type ctxStamp struct {
	field string
	col   string
	fn    string // import path, a dot and the function name
}

// writeScopeRule is the scope rule of a write, stricter than the one of a
// read: no scope column may come from the value. In a project that declares
// Postgres.Scopes, the table has a NOT NULL hard scope column, no nullable
// scope column, and the name names every scope column of the table: a hard
// scope binds a parameter, a context scope is its short name (stamped on an
// insert, filtered on an update). A table without a hard scope (no scope
// column, or a context scope only) would let a write reach every tenant.
func (r *resolver) writeScopeRule(m *derivedMethod, base string) *refusal {
	if !m.op.isWrite() || len(r.l.scopes) == 0 {
		return nil
	}
	hard, ctxs, all, fail := r.writeScopes()
	if fail != nil {
		return fail
	}
	if len(hard) == 0 {
		what := "has no scope column"
		if len(all) > 0 {
			what = "has the context scope " + strings.Join(all, " and ") + " only"
		}
		return &refusal{msg: fmt.Sprintf("table %s %s, and a derived write needs a NOT NULL hard scope "+
			"(Postgres.Scopes): without one it could reach the rows of every tenant", r.t.sqlName, what)}
	}
	var names, missing []string
	for _, f := range hard {
		names = append(names, r.display(f))
		if !slices.ContainsFunc(m.terms, func(t term) bool { return t.field == f }) {
			missing = append(missing, r.sqlName(f))
		}
	}
	for _, f := range ctxs {
		c := r.ctxOf(f)
		if c.short == "" {
			return &refusal{msg: fmt.Sprintf("a derived write stamps or filters the context scope %s, and "+
				"Postgres.ScopeNames gives it no short name", c.col),
				alt: "name " + c.col + " in Postgres.ScopeNames (such as \"Workspace\")"}
		}
		names = append(names, c.short)
		if !slices.ContainsFunc(m.terms, func(t term) bool { return t.field == f && t.ctx != nil }) {
			missing = append(missing, c.col)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	for _, t := range m.terms {
		if t.fromRow {
			names = append(names, t.field)
		}
	}
	return &refusal{
		msg: fmt.Sprintf("table %s has the scope columns %s, and the name does not name %s: a derived write "+
			"names every scope column, a hard scope as a parameter and a context scope by its short name, so "+
			"none comes from the value", r.t.sqlName, strings.Join(all, ", "), strings.Join(missing, " and ")),
		alt: "rename it " + suggestNamed(base, names)}
}

// writeScopes sorts the scope columns of the table into hard and context
// ones, with every column name; a nullable one is a refusal.
func (r *resolver) writeScopes() (hard, ctxs, all []string, fail *refusal) {
	for _, f := range r.allScope {
		col, _ := r.t.column(f)
		all = append(all, col.sqlName)
		switch {
		case col.nullable:
			return nil, nil, nil, &refusal{msg: fmt.Sprintf("table %s has the nullable scope column %s: a derived "+
				"write cannot bind a row that no tenant, or every tenant, owns", r.t.sqlName, col.sqlName)}
		case r.ctxOf(f) != nil:
			ctxs = append(ctxs, f)
		default:
			hard = append(hard, f)
		}
	}
	return hard, ctxs, all, nil
}

// checkWriteParams binds the parameters of a write: ctx, one parameter per
// scope column of the name, in name order, then the value, of the domain
// type of the declaration.
func (r *resolver) checkWriteParams(fn *types.Func, m *derivedMethod) *refusal {
	params := signatureOf(fn).Params()
	if params.Len() == 0 || !isNamed(params.At(0).Type(), "context", "Context") {
		return &refusal{msg: "the first parameter is not context.Context",
			alt: "take ctx context.Context as the first parameter"}
	}
	dom := r.qualified(r.t.domain)
	if got, want := params.Len()-1, len(m.preds)+1; got != want {
		alt := "take the value to write, " + dom + ", after ctx"
		if len(m.preds) > 0 {
			alt = "take one parameter per scope column, in name order (" + strings.Join(m.preds, ", ") +
				"), then the value to write, " + dom
		}
		return &refusal{msg: fmt.Sprintf("has %d value parameter(s); a derived %s takes %d", got, m.op, want),
			alt: alt}
	}
	used := map[string]bool{"ctx": true, "r": true, "err": true, "row": true, "stmt": true, "n": true,
		"found": true}
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
	last := params.At(params.Len() - 1)
	name := nameOr(last.Name(), params.Len()-2)
	if !types.Identical(last.Type(), r.t.domain) {
		return &refusal{
			msg: fmt.Sprintf("its last parameter %s is %s; a derived %s takes the domain type %s, which the "+
				"mapper returns", name, r.qualified(last.Type()), m.op, dom),
			alt: "take " + dom + ", or write the method by hand (an input type that is not the domain type)"}
	}
	for used[name] {
		name += "Arg"
	}
	m.params = append(m.params, param{name: name, typ: last.Type()})
	return nil
}

// writeShape checks the results of a write: the written row read back, or
// only an error. An update may also return an Option, absent when no row
// matched.
func (r *resolver) writeShape(results *types.Tuple, m *derivedMethod, wrong func(string) *refusal) *refusal {
	dom := r.qualified(r.t.domain)
	if results.Len() == 1 {
		m.shape = shapeErr
		return nil
	}
	if results.Len() == 2 {
		elem, s := rowOf(results.At(0).Type())
		if s == shapeOne || (s == shapeOption && m.op == opUpdate) {
			m.shape = s
			return r.sameDomain(elem)
		}
	}
	if m.op == opUpdate {
		return wrong(fmt.Sprintf("return (%s, error), (functional.Option[%s], error) or error", dom, dom))
	}
	return wrong(fmt.Sprintf("return (%s, error) or error", dom))
}

// checkWrite checks the rest of a write against the declaration and the
// table, and plans its columns: a ToRow, the stamps of its context scopes,
// the defaults of the database columns, a unique key for an update, the
// exact conflict key of an upsert, and a column to set.
func (r *resolver) checkWrite(m *derivedMethod) *refusal {
	if !r.b.writable() {
		return &refusal{altFirst: true,
			msg: fmt.Sprintf("a derived %s maps %s to a row, and the declaration has no nanorepo.ToRow", m.op,
				r.qualified(r.t.domain)),
			alt: "add nanorepo.ToRow(" + toRowName(r.t) + ") to the nanorepo.Implement of " +
				r.b.impl.Obj().Name() + ", a stateless function from " + r.qualified(r.t.domain) + " to " +
				r.qualified(r.t.row)}
	}
	plan := &writePlan{}
	m.write = plan
	for _, c := range r.t.columns {
		if slices.Contains(r.l.databaseColumns, c.sqlName) {
			plan.dbFields = append(plan.dbFields, c.field)
		}
	}
	if fail := r.checkWriteTerms(m); fail != nil {
		return fail
	}
	switch m.op {
	case opUpdate:
		if fail := r.checkUpdateKey(m); fail != nil {
			return fail
		}
	case opUpsert:
		if fail := r.checkConflictKey(m); fail != nil {
			return fail
		}
	case opCreate, opFind, opExists, opCount, opDelete:
	}
	if m.op != opUpdate {
		if fail := r.checkDefaults(m); fail != nil {
			return fail
		}
	}
	if m.op == opCreate {
		return nil
	}
	keep := slices.Concat(plan.dbFields, r.allScope)
	for _, t := range m.terms {
		keep = append(keep, t.field)
	}
	for _, c := range r.t.columns {
		if !c.primary && !slices.Contains(keep, c.field) {
			plan.set = append(plan.set, c.field)
		}
	}
	if len(plan.set) == 0 {
		return &refusal{msg: fmt.Sprintf("a derived %s on %s would set no column: the name, the scope, the "+
			"primary key and Postgres.DatabaseColumns hold every column", m.op, r.t.sqlName)}
	}
	return nil
}

// checkWriteTerms checks the columns a write names. A Create names scope
// columns only. A key column an update reads from its value is NOT NULL and
// has a predicate. Each context scope a Create or an Upsert stamps has a
// Stamp function.
func (r *resolver) checkWriteTerms(m *derivedMethod) *refusal {
	for _, t := range m.terms {
		col, _ := r.t.column(t.field)
		switch {
		case m.op == opCreate && t.fromRow:
			return &refusal{msg: fmt.Sprintf("CreateIn names scope columns only, and %s is not one", col.sqlName),
				alt: "drop " + t.field + " from the name: a derived Create inserts every column of the value"}
		case t.ctx != nil && m.op != opUpdate:
			fn := r.l.contextStamps[col.sqlName]
			if fn == "" {
				return &refusal{
					msg: fmt.Sprintf("a derived %s stamps %s from the context, and Postgres.ContextScopes gives "+
						"no Stamp function for it", m.op, col.sqlName),
					alt: "set Stamp in the ContextScope of " + col.sqlName + " in nanogen/project.go, " +
						"or write it by hand"}
			}
			m.write.stamps = append(m.write.stamps, ctxStamp{field: t.field, col: col.sqlName, fn: fn})
		case t.fromRow && m.op == opUpdate && (col.nullable || col.lit == litNone):
			return &refusal{msg: fmt.Sprintf("a derived update reads the key column %s from its value, and the "+
				"column is nullable or has no equality predicate", col.sqlName)}
		}
	}
	return nil
}

// checkUpdateKey proves that an update changes at most one row: the
// columns of its name, without the context scopes, which can read TRUE,
// cover the primary key or a key that UniqueKeys lists.
func (r *resolver) checkUpdateKey(m *derivedMethod) *refusal {
	if !r.t.keysFound {
		return &refusal{altFirst: true,
			msg: "a derived update changes one row, and the table package has no UniqueKeys for " + r.t.sqlName +
				" to prove it",
			alt: "run nanostack gen db, which writes UniqueKeys into the table package, then rerun nanostack gen repo"}
	}
	fields := keyFields(m)
	if k, ok := r.t.provingKey(fields); ok {
		m.key = &k
		return nil
	}
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = r.sqlName(f)
	}
	return &refusal{altFirst: true,
		msg: fmt.Sprintf("a derived update changes one row, but (%s) covers no unique key; %s",
			strings.Join(cols, ", "), r.t.keyList()),
		alt: "name the columns of a key after the scope, such as UpdateBy" + r.display(firstOr(r.scope, "Scope")) +
			"AndID, or write the update by hand on r.updateWhere"}
}

// checkConflictKey picks the conflict key of an upsert: one unique key, not
// partial, whose columns are among the columns of the name (without the
// context scopes), that holds every key column read from the value and a
// hard scope, so a conflict never reaches the row of another tenant. Every
// scope column outside the key guards the DO UPDATE: the existing row must
// hold the caller's value, which comes from a parameter or a stamp, never
// from the value.
func (r *resolver) checkConflictKey(m *derivedMethod) *refusal {
	if !r.t.keysFound {
		return &refusal{altFirst: true,
			msg: "a derived upsert needs a unique key, and the table package has no UniqueKeys for " + r.t.sqlName,
			alt: "run nanostack gen db, which writes UniqueKeys into the table package, then rerun nanostack gen repo"}
	}
	fields := keyFields(m)
	cols := make([]string, len(fields))
	var rowFields []string
	for i, f := range fields {
		cols[i] = r.sqlName(f)
		if !slices.Contains(r.allScope, f) {
			rowFields = append(rowFields, f)
		}
	}
	// The key: its columns are among the name's, it holds every key column
	// read from the value and a hard scope. A hard scope of the name outside
	// the key guards the DO UPDATE with the caller's parameter.
	var found []uniqueKey
	for _, k := range r.t.keys {
		if r.fitsConflict(k, fields, rowFields) {
			found = append(found, k)
		}
	}
	if len(found) > 1 {
		names := make([]string, len(found))
		for i, k := range found {
			names[i] = k.String()
		}
		return &refusal{msg: "the name matches more than one conflict key: " + strings.Join(names, ", "),
			alt: "write the upsert by hand"}
	}
	if len(found) == 1 {
		k := found[0]
		if k.pred != "" {
			return &refusal{msg: fmt.Sprintf("the conflict key %s is a partial index; ON CONFLICT would need its "+
				"predicate", k)}
		}
		m.key = &k
		m.write.target = slices.Clone(k.fields)
		for _, f := range r.allScope {
			if !slices.Contains(k.fields, f) {
				m.write.guards = append(m.write.guards, f)
			}
		}
		return nil
	}
	return &refusal{altFirst: true,
		msg: fmt.Sprintf("a derived upsert conflicts on one unique key that holds a hard scope and every key "+
			"column of the name, and no key fits (%s); %s",
			strings.Join(cols, ", "), r.t.keyList()),
		alt: "name the columns of one key, the scope first, or write the upsert by hand"}
}

// fitsConflict reports whether k can be the conflict key of an upsert whose
// name has fields, rowFields of them read from the value: its columns are
// among fields, it holds every one of rowFields, and a hard scope.
func (r *resolver) fitsConflict(k uniqueKey, fields, rowFields []string) bool {
	if slices.Contains(k.fields, "") {
		return false
	}
	within := !slices.ContainsFunc(k.fields, func(f string) bool { return !slices.Contains(fields, f) })
	holdsRow := !slices.ContainsFunc(rowFields, func(f string) bool { return !slices.Contains(k.fields, f) })
	hard := slices.ContainsFunc(k.fields, func(f string) bool {
		return slices.Contains(r.scope, f) && r.ctxOf(f) == nil
	})
	return within && holdsRow && hard
}

// checkDefaults checks that each database column an insert leaves out has a
// default, so the insert never writes NULL in its place.
func (r *resolver) checkDefaults(m *derivedMethod) *refusal {
	for _, f := range m.write.dbFields {
		col, _ := r.t.column(f)
		if !r.t.defaults[col.sqlName] {
			return &refusal{
				msg: fmt.Sprintf("%s is in Postgres.DatabaseColumns, and %s.%s has no default: a derived %s would "+
					"leave it NULL", col.sqlName, r.t.sqlName, col.sqlName, m.op),
				alt: "add a default to the column in a migration and rerun nanostack gen db, or write it by hand"}
		}
	}
	return nil
}

// keyFields are the columns of a write's name that select its row: every
// term except the context scopes named by their short name.
func keyFields(m *derivedMethod) []string {
	var out []string
	for _, t := range m.terms {
		if t.ctx == nil {
			out = append(out, t.field)
		}
	}
	return out
}

// toRowName suggests the name of a ToRow function: itemToRow for the
// domain type Item.
func toRowName(t *tableBase) string {
	if n, ok := types.Unalias(t.domain).(*types.Named); ok {
		return lowerFirst(n.Obj().Name()) + "ToRow"
	}
	return lowerFirst(t.goName) + "ToRow"
}

func firstOr(s []string, or string) string {
	if len(s) == 0 {
		return or
	}
	return s[0]
}

// writeSQL is the statement a derived write runs, for its comment and
// -explain.
func (r *resolver) writeSQL(m *derivedMethod) string {
	returning := " RETURNING *"
	if m.shape == shapeErr {
		returning = ""
	}
	written := func() []string {
		var out []string
		for _, c := range r.t.columns {
			if !slices.Contains(m.write.dbFields, c.field) {
				out = append(out, c.sqlName)
			}
		}
		return out
	}
	names := func(fields []string) []string {
		out := make([]string, len(fields))
		for i, f := range fields {
			out[i] = r.sqlName(f)
		}
		return out
	}
	switch m.op {
	case opCreate:
		return "INSERT INTO " + r.t.sqlName + " (" + strings.Join(written(), ", ") + ") VALUES (...)" + returning
	case opUpsert:
		sets := make([]string, len(m.write.set))
		for i, f := range m.write.set {
			sets[i] = r.sqlName(f) + " = EXCLUDED." + r.sqlName(f)
		}
		s := "INSERT INTO " + r.t.sqlName + " (" + strings.Join(written(), ", ") + ") VALUES (...) ON CONFLICT (" +
			strings.Join(names(m.write.target), ", ") + ") DO UPDATE SET " + strings.Join(sets, ", ")
		if len(m.write.guards) > 0 {
			guards := make([]string, len(m.write.guards))
			for i, f := range m.write.guards {
				guards[i] = r.t.sqlName + "." + r.sqlName(f) + " = EXCLUDED." + r.sqlName(f)
			}
			s += " WHERE " + strings.Join(guards, " AND ")
		}
		return s + returning
	case opUpdate, opFind, opExists, opCount, opDelete:
	}
	parts := make([]string, len(m.terms))
	next := 0
	for i, t := range m.terms {
		switch {
		case t.ctx != nil:
			parts[i] = funcWord(t.ctx.fn) + "(ctx, " + t.ctx.col + ")"
		case t.fromRow:
			parts[i] = r.sqlName(t.field) + " = <value>." + t.field
		default:
			next++
			parts[i] = r.sqlName(t.field) + " = $" + strconv.Itoa(next)
		}
	}
	return "UPDATE " + r.t.sqlName + " SET " + strings.Join(names(m.write.set), ", ") + " WHERE " +
		strings.Join(parts, " AND ") + returning
}
