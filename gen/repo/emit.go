package genrepo

import (
	"fmt"
	"go/format"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
)

// emitter writes one repo_base.gen.go.
type emitter struct {
	b    strings.Builder
	imp  *importset.Set
	self string // import path of the declaring package
}

func (e *emitter) p(format string, args ...any) { fmt.Fprintf(&e.b, format, args...) }

// ref returns pkg.Name for an identifier of package path, importing it.
func (e *emitter) ref(path, name, ident string) string {
	return e.imp.Add(path, name) + "." + ident
}

func (e *emitter) pg(ident string) string { return e.ref(postgresPath, "postgres", ident) }

func (e *emitter) typ(t types.Type) string { return types.TypeString(t, e.imp.Qualifier) }

// render returns the formatted repo_base.gen.go of the declarations.
func (l *loaded) render(d *declarations) ([]byte, error) {
	e := &emitter{imp: importset.New(l.pkg.PkgPath), self: l.pkg.PkgPath}
	e.imp.Block(l.pkg.Types.Scope().Names()...)
	tables := slices.Clone(d.tables)
	slices.SortFunc(tables, func(a, b *tableBase) int { return strings.Compare(a.baseName(), b.baseName()) })
	for _, t := range tables {
		e.base(t)
		e.where(t)
	}
	var out strings.Builder
	out.WriteString(header)
	out.WriteString("package " + l.pkg.Name + "\n\n")
	e.imp.Write(&out)
	out.WriteString(e.b.String())
	src, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, gen.Errorf("", 0, "report this nano gen repo bug with the declaration that caused it",
			"the generated base does not parse: %v", err)
	}
	return src, nil
}

// mapperName renders the mapper of t as the base calls it.
func (e *emitter) mapperName(t *tableBase) string {
	obj := t.mapper.obj
	if obj.Pkg().Path() == e.self {
		return obj.Name()
	}
	return e.ref(obj.Pkg().Path(), obj.Pkg().Name(), obj.Name())
}

// base writes the base struct of t, its constructor, its helpers and its
// transactor methods.
func (e *emitter) base(t *tableBase) {
	base := t.baseName()
	tbl := e.ref(t.pkgPath, t.pkgName, t.goName)
	tblType := e.typ(t.tableType)
	db := e.ref(qrmPath, "qrm", "DB")
	e.p("// %s is the generated base of table %s. It holds the table (t),\n", base, t.sqlName)
	e.p("// typed predicates (where), helpers that run in the context transaction and\n")
	if t.mapper == nil {
		e.p("// return the model rows, and the pkg/db/transactor functions as methods.\n")
	} else {
		e.p("// map each row with %s, and the pkg/db/transactor functions as methods.\n", t.mapper.obj.Name())
	}
	e.p("// Embed it in the implementation and set it with %s(db).\n", t.newName())
	e.p("type %s struct {\n\tdb %s\n\tt %s\n\twhere %s\n}\n\n", base, db, tblType, t.whereName())
	e.p("// %s returns the base of table %s. It runs its statements on the\n", t.newName(), t.sqlName)
	e.p("// transaction of the context, and on db outside one.\n")
	e.p("func %s(db %s) %s {\n\treturn %s{db: db, t: %s}\n}\n\n", t.newName(), db, base, base, tbl)

	e.reads(t)
	e.writes(t)
	e.hatches(t)
}

// reads writes the helpers of a base that read rows.
func (e *emitter) reads(t *tableBase) {
	base, ctx := t.baseName(), e.ref("context", "context", "Context")
	bexpr, order := e.pg("BoolExpression"), e.pg("OrderByClause")
	opt := e.ref(functionalPath, "functional", "Option")
	fn := e.imp.Add(functionalPath, "functional")
	dom := e.typ(t.domain)
	selectAll := "\tstmt := b.t.SELECT(b.t.AllColumns)\n\tif cond != nil {\n\t\tstmt = stmt.WHERE(cond)\n\t}\n"
	e.p("// findOne returns the row matching cond: absent when none does, and\n")
	e.p("// transactor.ErrMoreThanOneRow when two do. It reads LIMIT 2, so a key that\n")
	e.p("// stopped being unique is an error, never a silent first row. To pick one of\n")
	e.p("// several rows, use first with an order.\n")
	e.p("func (b %s) findOne(ctx %s, cond %s) (%s[%s], error) {\n", base, ctx, bexpr, opt, dom)
	e.p("%s\trows, err := b.queryRows(ctx, stmt.LIMIT(2)).Value()\n\tswitch {\n\tcase err != nil:\n", selectAll)
	e.p("\t\treturn %s.None[%s](), err\n\tcase len(rows) > 1:\n", fn, dom)
	e.p("\t\treturn %s.None[%s](), %s(\"%%w: %s\", %s)\n", fn, dom, e.ref("fmt", "fmt", "Errorf"), t.sqlName,
		e.ref(transactorPath, "transactor", "ErrMoreThanOneRow"))
	e.p("\tcase len(rows) == 0:\n\t\treturn %s.None[%s](), nil\n\t}\n", fn, dom)
	e.p("\treturn %s.Some(rows[0]), nil\n}\n\n", fn)

	e.p("// first returns the first row matching cond in the given order, absent when\n")
	e.p("// none does. End the order with a unique column, such as the primary key, so\n")
	e.p("// the row it picks is stable.\n")
	e.p("func (b %s) first(ctx %s, cond %s, order %s, more ...%s) (%s[%s], error) {\n",
		base, ctx, bexpr, order, order, opt, dom)
	e.p("%s\treturn b.queryRowOptional(ctx, stmt.ORDER_BY(append([]%s{order}, more...)...).LIMIT(1))\n}\n\n",
		selectAll, order)

	e.p("// findAll returns every row matching cond, every row when cond is nil, in the\n")
	e.p("// given order.\n")
	e.p("func (b %s) findAll(ctx %s, cond %s, order ...%s) ([]%s, error) {\n", base, ctx, bexpr, order, dom)
	e.p("%s\tif len(order) > 0 {\n\t\tstmt = stmt.ORDER_BY(order...)\n\t}\n", selectAll)
	e.p("\treturn b.queryRows(ctx, stmt).Value()\n}\n\n")

	e.p("// exists reports whether a row matches cond, or whether the table has a row\n")
	e.p("// when cond is nil. PostgreSQL refuses a row lock on it, which aggregates.\n")
	e.p("func (b %s) exists(ctx %s, cond %s) (bool, error) {\n", base, ctx, bexpr)
	e.p("\tfound := b.t.SELECT(%s(1))\n\tif cond != nil {\n\t\tfound = found.WHERE(cond)\n\t}\n", e.pg("Int32"))
	e.p("\tn, err := b.queryCount(ctx, %s(%s(%s)).FROM(found.LIMIT(1).AsTable(\"found\"))).Value()\n",
		e.pg("SELECT"), e.pg("COUNT"), e.pg("STAR"))
	e.p("\treturn n > 0, err\n}\n\n")

	e.p("// count counts the rows matching cond, every row when cond is nil.\n")
	e.p("// PostgreSQL refuses a row lock on it, which aggregates.\n")
	e.p("func (b %s) count(ctx %s, cond %s) (int64, error) {\n", base, ctx, bexpr)
	e.p("\tstmt := b.t.SELECT(%s(%s))\n\tif cond != nil {\n\t\tstmt = stmt.WHERE(cond)\n\t}\n",
		e.pg("COUNT"), e.pg("STAR"))
	e.p("\treturn b.queryCount(ctx, stmt).Value()\n}\n\n")
}

// writes writes the helpers of a base that change rows.
func (e *emitter) writes(t *tableBase) {
	base, ctx := t.baseName(), e.ref("context", "context", "Context")
	bexpr := e.pg("BoolExpression")
	errorsNew := e.ref("errors", "errors", "New")
	e.p("// deleteWhere deletes the rows matching cond and returns how many it deleted.\n")
	e.p("// It refuses a nil cond. A locked context returns transactor.ErrRowLockNotSelect\n")
	e.p("// or ErrRowLockOutsideTx without running the DELETE.\n")
	e.p("func (b %s) deleteWhere(ctx %s, cond %s) (int64, error) {\n", base, ctx, bexpr)
	e.p("\tif cond == nil {\n\t\treturn 0, %s(%q)\n\t}\n", errorsNew, t.sqlName+": deleteWhere needs a condition")
	e.p("\treturn b.execAffected(ctx, b.t.DELETE().WHERE(cond)).Value()\n}\n\n")

	assign := e.pg("ColumnAssigment")
	e.p("// updateWhere applies the assignments, such as b.t.Name.SET(postgres.String(v)),\n")
	e.p("// to the rows matching cond and returns how many it updated. It refuses a nil\n")
	e.p("// cond, and a locked context, as deleteWhere does.\n")
	e.p("func (b %s) updateWhere(ctx %s, cond %s, set %s, more ...%s) (int64, error) {\n",
		base, ctx, bexpr, assign, assign)
	e.p("\tif cond == nil {\n\t\treturn 0, %s(%q)\n\t}\n", errorsNew, t.sqlName+": updateWhere needs a condition")
	e.p("\tvalues := make([]any, len(more))\n\tfor i, m := range more {\n\t\tvalues[i] = m\n\t}\n")
	e.p("\treturn b.execAffected(ctx, b.t.UPDATE().SET(set, values...).WHERE(cond)).Value()\n}\n\n")
}

// hatches writes the transactor functions as methods of the base. Each one
// only forwards, so the context transaction and row locks behave as in the
// transactor.
func (e *emitter) hatches(t *tableBase) {
	base, ctx := t.baseName(), e.ref("context", "context", "Context")
	stmt := e.pg("Statement")
	result := e.ref(transactorPath, "transactor", "Result")
	tx := e.imp.Add(transactorPath, "transactor")
	opt := e.ref(functionalPath, "functional", "Option")
	dom, row := e.typ(t.domain), e.typ(t.row)

	e.p("// query runs stmt and scans its rows into T: a model row, a slice, or a struct\n")
	e.p("// of aliased columns. Chain OnUnique, OnForeignKey or OnNoRows before Value.\n")
	e.p("func (b %s) query[T any](ctx %s, stmt %s) %s[T] {\n\treturn %s.Query[T](ctx, b.db, stmt)\n}\n\n",
		base, ctx, stmt, result, tx)
	e.p("// queryOptional runs stmt, which may match no row: then the Option is absent\n// and the error nil.\n")
	e.p("func (b %s) queryOptional[T any](ctx %s, stmt %s) (%s[T], error) {\n", base, ctx, stmt, opt)
	e.p("\treturn %s.QueryOptional[T](ctx, b.db, stmt)\n}\n\n", tx)
	e.p("// queryMap runs stmt, scans one T and maps it with mapFunc.\n")
	e.p("func (b %s) queryMap[T, R any](ctx %s, stmt %s, mapFunc func(T) R) %s[R] {\n", base, ctx, stmt, result)
	e.p("\treturn %s.QueryMap(ctx, b.db, stmt, mapFunc)\n}\n\n", tx)
	e.p("// queryOptionalMap is queryOptional, then mapFunc on the row when there is one.\n")
	e.p("func (b %s) queryOptionalMap[T, R any](ctx %s, stmt %s, mapFunc func(T) R) (%s[R], error) {\n",
		base, ctx, stmt, opt)
	e.p("\treturn %s.QueryOptionalMap(ctx, b.db, stmt, mapFunc)\n}\n\n", tx)
	e.p("// queryMapSlice runs stmt and maps every row with mapFunc.\n")
	e.p("func (b %s) queryMapSlice[T, R any](ctx %s, stmt %s, mapFunc func(T) R) %s[[]R] {\n",
		base, ctx, stmt, result)
	e.p("\treturn %s.QueryMapSlice(ctx, b.db, stmt, mapFunc)\n}\n\n", tx)
	e.p("// queryCount runs a SELECT COUNT statement.\n")
	e.p("func (b %s) queryCount(ctx %s, stmt %s) %s[int64] {\n\treturn %s.QueryCount(ctx, b.db, stmt)\n}\n\n",
		base, ctx, stmt, result, tx)
	e.p("// exec runs a statement that returns no rows. Unwrap it with Err.\n")
	e.p("func (b %s) exec(ctx %s, stmt %s) %s[struct{}] {\n\treturn %s.Exec(ctx, b.db, stmt)\n}\n\n",
		base, ctx, stmt, result, tx)
	e.p("// execAffected runs a statement and returns the number of rows it changed.\n")
	e.p("func (b %s) execAffected(ctx %s, stmt %s) %s[int64] {\n\treturn %s.ExecAffected(ctx, b.db, stmt)\n}\n\n",
		base, ctx, stmt, result, tx)
	e.p("// page starts a paginated query of rows T mapped to R; both come from mapFunc.\n")
	e.p("func (b %s) page[T, R any](mapFunc func(T) R, columns ...%s) *%s[T, R] {\n",
		base, e.pg("Projection"), e.ref(transactorPath, "transactor", "PageBuilder"))
	e.p("\treturn %s.Page(b.db, mapFunc, columns...)\n}\n\n", tx)

	if t.mapper == nil {
		e.p("// queryRow runs stmt, which returns one %s row, such as an INSERT or\n", t.sqlName)
		e.p("// UPDATE ... RETURNING(b.t.AllColumns), and returns the row.\n")
		e.p("func (b %s) queryRow(ctx %s, stmt %s) %s[%s] {\n\treturn b.query[%s](ctx, stmt)\n}\n\n",
			base, ctx, stmt, result, row, row)
		e.p("// queryRowOptional is queryRow for a statement that may return no row.\n")
		e.p("func (b %s) queryRowOptional(ctx %s, stmt %s) (%s[%s], error) {\n", base, ctx, stmt, opt, row)
		e.p("\treturn b.queryOptional[%s](ctx, stmt)\n}\n\n", row)
		e.p("// queryRows is queryRow for every row stmt returns; no row is an empty slice.\n")
		e.p("func (b %s) queryRows(ctx %s, stmt %s) %s[[]%s] {\n", base, ctx, stmt, result, row)
		e.p("\treturn b.queryMapSlice(ctx, stmt, func(row %s) %s { return row })\n}\n\n", row, row)
		return
	}
	m := e.mapperName(t)
	e.p("// queryRow runs stmt, which returns one %s row, such as an INSERT or\n", t.sqlName)
	e.p("// UPDATE ... RETURNING(b.t.AllColumns), and maps it with %s.\n", t.mapper.obj.Name())
	e.p("func (b %s) queryRow(ctx %s, stmt %s) %s[%s] {\n\treturn b.queryMap(ctx, stmt, %s)\n}\n\n",
		base, ctx, stmt, result, dom, m)
	e.p("// queryRowOptional is queryRow for a statement that may return no row.\n")
	e.p("func (b %s) queryRowOptional(ctx %s, stmt %s) (%s[%s], error) {\n", base, ctx, stmt, opt, dom)
	e.p("\treturn b.queryOptionalMap(ctx, stmt, %s)\n}\n\n", m)
	e.p("// queryRows is queryRow for every row stmt returns; no row is an empty slice.\n")
	e.p("func (b %s) queryRows(ctx %s, stmt %s) %s[[]%s] {\n\treturn b.queryMapSlice(ctx, stmt, %s)\n}\n\n",
		base, ctx, stmt, result, dom, m)
}

// litExpr renders the Nanostack DB literal of the value v for a column.
func (e *emitter) litExpr(c column) string {
	if c.lit == litEnum {
		return e.pg("StringExp") + "(" + e.pg("CAST") + "(" + e.pg("String") + "(string(v))).AS(" +
			strconv.Quote(c.enum) + "))"
	}
	return e.pg(literalName(c.lit)) + "(v)"
}

// literalName is the Nanostack DB constructor of a literal other than an
// enum.
func literalName(l literal) string {
	switch l {
	case litString:
		return "String"
	case litUUID:
		return "UUID"
	case litInt8:
		return "Int8"
	case litInt16:
		return "Int16"
	case litInt32:
		return "Int32"
	case litInt64:
		return "Int64"
	case litReal:
		return "Real"
	case litDouble:
		return "Double"
	case litBool:
		return "Bool"
	case litTimestampz:
		return "TimestampzT"
	case litTimestamp:
		return "TimestampT"
	case litDate:
		return "DateT"
	case litTime:
		return "TimeT"
	case litTimez:
		return "TimezT"
	default:
		return ""
	}
}

// where writes the predicate type of t.
func (e *emitter) where(t *tableBase) {
	where, tbl := t.whereName(), e.ref(t.pkgPath, t.pkgName, t.goName)
	bexpr := e.pg("BoolExpression")
	var skipped []string
	for _, c := range t.columns {
		if c.lit == litNone {
			skipped = append(skipped, c.sqlName)
		}
	}
	e.p("// %s builds predicates on the columns of %s. A string predicate\n", where, t.sqlName)
	e.p("// compares as text. Contains is searchexec.Contains: it trims the term,\n")
	e.p("// matches a substring without case, and %% _ ! in the term match\n")
	e.p("// themselves. A blank term matches every row.\n")
	if len(skipped) > 0 {
		e.p("//\n// No equality predicate on %s: compare on b.t.\n", strings.Join(skipped, ", "))
	}
	e.p("type %s struct{}\n\n", where)
	for _, c := range t.columns {
		col := tbl + "." + c.field
		if c.lit != litNone {
			vt := e.typ(c.value)
			e.p("// %s is %s = v.\n", c.field, c.sqlName)
			e.p("func (%s) %s(v %s) %s { return %s.EQ(%s) }\n\n", where, c.field, vt, bexpr, col, e.litExpr(c))
			e.p("// %sIn is %s IN (vs...); no value matches no row.\n", c.field, c.sqlName)
			e.p("func (%s) %sIn(vs ...%s) %s {\n\tif len(vs) == 0 {\n\t\treturn %s(false)\n\t}\n",
				where, c.field, vt, bexpr, e.pg("Bool"))
			e.p("\texprs := make([]%s, len(vs))\n\tfor i, v := range vs {\n\t\texprs[i] = %s\n\t}\n",
				e.pg("Expression"), e.litExpr(c))
			e.p("\treturn %s.IN(exprs...)\n}\n\n", col)
		}
		if c.textual() {
			e.p("// %sContains matches %s that contain term, ignoring case.\n", c.field, c.sqlName)
			e.p("func (%s) %sContains(term string) %s {\n", where, c.field, bexpr)
			e.p("\treturn %s(term, %s)\n}\n\n", e.ref(searchexecPath, "searchexec", "Contains"), col)
			e.p("// %sEqualsIgnoreCase is lower(%s) = lower(v).\n", c.field, c.sqlName)
			e.p("func (%s) %sEqualsIgnoreCase(v string) %s {\n", where, c.field, bexpr)
			e.p("\treturn %s(%s).EQ(%s(%s(v)))\n}\n\n", e.pg("LOWER"), col, e.pg("LOWER"), e.pg("String"))
		}
		if c.lit.ordered() {
			vt := e.typ(c.value)
			e.p("// %sAfter is %s > v.\n", c.field, c.sqlName)
			e.p("func (%s) %sAfter(v %s) %s { return %s.GT(%s) }\n\n", where, c.field, vt, bexpr, col, e.litExpr(c))
			e.p("// %sBefore is %s < v.\n", c.field, c.sqlName)
			e.p("func (%s) %sBefore(v %s) %s { return %s.LT(%s) }\n\n", where, c.field, vt, bexpr, col, e.litExpr(c))
		}
		if c.nullable {
			e.p("// %sIsNull is %s IS NULL.\n", c.field, c.sqlName)
			e.p("func (%s) %sIsNull() %s { return %s.IS_NULL() }\n\n", where, c.field, bexpr, col)
		}
	}
}
