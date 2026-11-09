package genrepo

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// writer renders the body of one derived write.
type writer struct {
	e   *emitter
	m   *derivedMethod
	b   *binding
	via string
	s   strings.Builder
}

func (w *writer) p(format string, args ...any) { fmt.Fprintf(&w.s, format, args...) }

// col renders a column of the table of the base.
func (w *writer) col(field string) string { return w.via + "t." + field }

// writeBody renders a derived write (Create, CreateIn, UpdateBy, UpsertBy)
// on the base: the value mapped with ToRow; the scope parameters and the
// context stamps set on the row of a Create or an Upsert; the statement,
// which never writes Postgres.DatabaseColumns; and the result, with the
// OnUnique rules of the declaration.
func (e *emitter) writeBody(m *derivedMethod, b *binding, via string) string {
	w := &writer{e: e, m: m, b: b, via: via}
	cond := w.row()
	switch m.op {
	case opCreate:
		w.p("\tstmt := %s.INSERT(%s).\n\t\tMODEL(row)", via+"t", w.inserted())
	case opUpdate:
		w.p("\tstmt := %s.UPDATE(%s).\n\t\tMODEL(row).\n\t\tWHERE(%s)", via+"t", w.cols(m.write.set), andAll(cond))
	case opUpsert:
		w.upsert()
	case opFind, opExists, opCount, opDelete:
		panic("writeBody on a " + m.op.String())
	}
	w.result()
	return w.s.String()
}

// row maps the value to the row, sets the scope parameters and the context
// stamps of a Create or an Upsert on it, and returns the conditions of the
// name for an update.
func (w *writer) row() []string {
	m := w.m
	value := m.params[len(m.params)-1].name
	if w.b.toRow != nil {
		w.p("\trow := %s(%s)\n", w.e.fn(w.b.toRow.obj), value)
	} else {
		w.p("\trow := %s\n", value)
	}
	var cond []string
	next := 0
	for _, term := range m.terms {
		switch {
		case term.ctx != nil:
			cond = append(cond, w.e.ctxCall(term.ctx, w.via))
		case term.fromRow:
			cond = append(cond, w.via+"where."+term.field+"(row."+term.field+")")
		default:
			p := m.params[next]
			next++
			arg := p.name
			if p.conv != nil {
				arg = w.e.typ(p.conv) + "(" + arg + ")"
			}
			cond = append(cond, w.via+"where."+term.field+"("+arg+")")
			if m.op != opUpdate {
				w.p("\trow.%s = %s\n", term.field, arg)
			}
		}
	}
	if len(m.write.stamps) > 0 {
		w.p("\tvar err error\n")
		for _, st := range m.write.stamps {
			w.p("\tif row.%s, err = %s(ctx); err != nil {\n\t\treturn %s\n\t}\n", st.field, w.e.funcRef(st.fn),
				w.zeroErr())
		}
	}
	return cond
}

// upsert renders INSERT ... ON CONFLICT (key) DO UPDATE SET ..., guarded
// by the scope columns outside the key.
func (w *writer) upsert() {
	plan := w.m.write
	w.p("\tstmt := %s.INSERT(%s).\n\t\tMODEL(row).\n\t\tON_CONFLICT(%s).\n\t\tDO_UPDATE(%s(\n",
		w.via+"t", w.inserted(), w.cols(plan.target), w.e.pg("SET"))
	for _, f := range plan.set {
		w.p("\t\t\t%s.SET(%s),\n", w.col(f), w.col("EXCLUDED."+f))
	}
	w.p("\t\t)")
	if len(plan.guards) > 0 {
		guards := make([]string, len(plan.guards))
		for i, f := range plan.guards {
			op := "EQ"
			if c, _ := w.b.table.column(f); c.nullable {
				op = "IS_NOT_DISTINCT_FROM"
			}
			guards[i] = w.col(f) + "." + op + "(" + w.col("EXCLUDED."+f) + ")"
		}
		w.p(".WHERE(%s)", andAll(guards))
	}
	w.p(")")
}

// result reads the row back with RETURNING, or runs the statement for its
// row count, and maps the errors: OnUnique, and no row (an update, or an
// upsert that met a row of another scope) as sql.ErrNoRows.
func (w *writer) result() {
	m, e, via := w.m, w.e, w.via
	maps := e.onUnique(w.b.onUnique)
	noRows := e.ref("database/sql", "sql", "ErrNoRows")
	if m.op != opCreate {
		maps += ".\n\t\tOnNoRows(" + noRows + ")"
	}
	switch m.shape {
	case shapeOne:
		w.p(".\n\t\tRETURNING(%s)\n\treturn %squeryRow(ctx, stmt)%s.\n\t\tValue()\n", w.col("AllColumns"), via, maps)
	case shapeOption:
		none := e.ref(functionalPath, "functional", "None") + "[" + e.typ(w.b.table.domain) + "]()"
		w.p(".\n\t\tRETURNING(%s)\n\tfound, err := %squeryRow(ctx, stmt)%s.\n\t\tValue()\n", w.col("AllColumns"),
			via, maps)
		w.p("\tif %s(err, %s) {\n\t\treturn %s, nil\n\t}\n", e.ref("errors", "errors", "Is"), noRows, none)
		w.p("\tif err != nil {\n\t\treturn %s, err\n\t}\n", none)
		w.p("\treturn %s(found), nil\n", e.ref(functionalPath, "functional", "Some"))
	case shapeErr:
		if m.op == opCreate {
			w.p("\n\treturn %sexecAffected(ctx, stmt)%s.\n\t\tErr()\n", via, e.onUnique(w.b.onUnique))
			return
		}
		w.p("\n\tn, err := %sexecAffected(ctx, stmt)%s.\n\t\tValue()\n", via, e.onUnique(w.b.onUnique))
		w.p("\tif err == nil && n == 0 {\n\t\terr = %s\n\t}\n\treturn err\n", noRows)
	case shapeSlice, shapeBool, shapeInt64, shapeDeleted, shapeAffected:
		panic("a derived write has no " + strconv.Itoa(int(m.shape)) + " shape")
	}
}

// inserted renders the columns an insert writes: every column but the
// database columns.
func (w *writer) inserted() string {
	if len(w.m.write.dbFields) == 0 {
		return w.col("AllColumns")
	}
	return w.col("AllColumns") + ".Except(" + w.cols(w.m.write.dbFields) + ")"
}

func (w *writer) cols(fields []string) string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = w.col(f)
	}
	return strings.Join(out, ", ")
}

// zeroErr renders the results of a write that returns err before its
// statement runs.
func (w *writer) zeroErr() string {
	if w.m.shape == shapeErr {
		return "err"
	}
	return "*new(" + w.e.typ(w.b.table.domain) + "), err"
}

// onUnique renders the OnUnique calls of a declaration, chained on a
// transactor.Result.
func (e *emitter) onUnique(rules []onUniqueRule) string {
	var s strings.Builder
	for _, rule := range rules {
		names := make([]string, len(rule.constraints))
		for i, c := range rule.constraints {
			names[i] = strconv.Quote(c)
		}
		s.WriteString(".\n\t\tOnUnique(" + e.fn(rule.target) + ", " + strings.Join(names, ", ") + ")")
	}
	return s.String()
}

// funcRef renders a function registered in the project file, written as its
// import path, a dot and its name.
func (e *emitter) funcRef(fn string) string {
	pkg, ident, found := strings.CutLast(fn, ".")
	if !found {
		return fn
	}
	if pkg == e.self {
		return ident
	}
	return e.ref(pkg, path.Base(pkg), ident)
}
