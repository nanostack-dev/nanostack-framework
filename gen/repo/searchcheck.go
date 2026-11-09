package genrepo

import (
	"go/types"
	"slices"
	"strings"
)

// textFix is the fix of a text operator on a column that is not text (E2).
const textFix = "use nanorepo.Range(...), Eq or In, or nanorepo.FullTextExpr on a typed expression " +
	"(CAST(...).AS_TEXT(), host(...))"

// checkSearch checks a search that reads without a problem: the columns
// of the options (E5), the text operators (E2), FilterAs (E13), the default
// sort (E14) and the order (E3, E4).
func (c *collector) checkSearch(s *searchDecl) {
	if !c.checkColumnsRead(s) {
		return
	}
	for _, f := range s.filters {
		c.checkFilter(f)
	}
	for _, col := range s.fullText {
		if typ := col.table.catalog[col.col.sqlName]; !textType(typ) {
			c.errorf(col.expr, "remove the column from FullText, or match a typed expression with "+
				"nanorepo.FullTextExpr (CAST(...).AS_TEXT(), host(...))",
				"FullText(%s): column %s (%s) is not a text column", col, col.col.sqlName, typ)
		}
	}
	c.checkDefaultSort(s)
	c.order(s)
}

// checkFilter checks the text operators and the value type of one filter.
// The operators of FilterExists apply to its child column, so they get the
// same check. InFold and ContainsAny get the same check with their field
// types (G11).
func (c *collector) checkFilter(f *searchFilter) {
	if f.col == nil {
		return
	}
	if f.as != nil && !types.Identical(f.as.Underlying(), f.col.col.value.Underlying()) {
		model := c.qualified(f.col.col.value)
		c.errorf(f.node, "declare "+c.qualified(f.as)+" over "+model+", or use nanorepo.Filter("+
			c.filterArgs(f)+"), with values of the model type",
			"FilterAs[%s] on %s: the model field is %s, and %s is %s", c.qualified(f.as), f.col.col.sqlName,
			model, c.qualified(f.as), c.qualified(f.as.Underlying()))
	}
	typ := f.col.table.catalog[f.col.col.sqlName]
	for _, op := range f.ops {
		if op.name == "Contains" && !textType(typ) {
			c.errorf(op.node, textFix, "%s(%s, %s): column %s (%s) is not a text column", f.kind, f.col,
				c.source(op.node), f.col.col.sqlName, typ)
		}
	}
}

// filterArgs renders the arguments of a filter as written.
func (c *collector) filterArgs(f *searchFilter) string {
	args := []string{f.col.String()}
	for _, op := range f.ops {
		args = append(args, c.source(op.node))
	}
	return strings.Join(args, ", ")
}

// checkDefaultSort checks that DefaultSort names a sort of the search, and
// DefaultSortExpr the field of a SortExpr (E14).
func (c *collector) checkDefaultSort(s *searchDecl) {
	d := s.defaultSort
	if d == nil {
		return
	}
	for _, sort := range s.sorts {
		if d.col != nil && sort.col != nil && sort.col.table == d.col.table && sort.col.alias == d.col.alias &&
			sort.col.col.field == d.col.col.field {
			return
		}
		if d.col == nil && sort.kind == "SortExpr" && sort.field == d.field {
			return
		}
	}
	var names []string
	for _, sort := range s.sorts {
		switch {
		case sort.col != nil:
			names = append(names, c.qualifiedColumn(s, sort.col))
		case sort.kind == "SortExpr":
			names = append(names, "SortExpr "+sort.field)
		default:
			names = append(names, sort.api...)
		}
	}
	list := "it has none"
	if len(names) > 0 {
		list = strings.Join(names, ", ")
	}
	if d.col != nil {
		c.errorf(d.node, "add nanorepo.Sort("+d.col.String()+"), or sort by default on one of the sorts",
			"search %s: DefaultSort(%s) is not one of its sorts (%s)", s.name, c.qualifiedColumn(s, d.col), list)
		return
	}
	c.errorf(d.node, `add nanorepo.SortExpr("`+d.field+`", fn), where fn returns the expression, `+
		"or sort by default on one of the sorts",
		"search %s: DefaultSortExpr(%q) names no SortExpr field of the search (sorts: %s)", s.name, d.field, list)
}

// order sets the tiebreak of the search: the columns of Tiebreak, which
// must cover a unique key of the searched table (E4), or its primary key in
// index order (E3). A key covers when the list holds every one of its
// columns, in any place, among other columns or not (design S7). Only a key
// that orders every row counts: a partial key, or a key with a column that
// can be NULL, lets rows repeat. For the same reason, the table of the
// search must not be on the nullable side of a LEFT JOIN.
func (c *collector) order(s *searchDecl) {
	t := s.root.table
	if s.root.nullable {
		c.errorf(s.from.arg, "search the table the join starts from, or join "+t.sqlName+" with INNER_JOIN",
			"search %s: %s is on the nullable side of a LEFT JOIN in %s, so no key of it orders every row",
			s.name, t.sqlName, s.from.name)
		return
	}
	if _, given := s.given["Tiebreak"]; !given {
		pk, ok := c.primaryKey(s, t)
		if !ok {
			return
		}
		s.order, s.orderKey, s.orderDefault = keyColumns(t, pk), pk, true
		return
	}
	if len(s.tiebreak) == 0 {
		c.errorf(s.tiebreakNode, c.tiebreakFix(s, nil),
			"search %s: Tiebreak() names no column, so it covers no unique key of %s; %s", s.name, t.sqlName,
			keysOf(t))
		return
	}
	var fields []string
	for _, col := range s.tiebreak {
		if slices.Contains(fields, col.col.field) && s.root.is(col.table, col.alias) {
			c.errorf(col.expr, "name each column once",
				"search %s: Tiebreak names %s twice", s.name, col)
			return
		}
		if !s.root.is(col.table, col.alias) {
			c.errorf(col.expr, "name columns of "+t.sqlName+" that cover one of its unique keys, "+
				"or remove Tiebreak to use the primary key",
				"search %s: Tiebreak(%s) names %s; a tiebreak orders the rows of %s, the table of the search",
				s.name, col, s.instanceOf(col), t.sqlName)
			return
		}
		fields = append(fields, col.col.field)
		s.order = append(s.order, col.col)
	}
	for _, k := range keysPrimaryFirst(t) { // so -explain names the primary key when it is covered
		if ordersEveryRow(t, k) == "" && !slices.ContainsFunc(k.fields, func(f string) bool {
			return !slices.Contains(fields, f)
		}) {
			s.orderKey = k
			return
		}
	}
	c.errorf(s.tiebreak[0].expr, c.tiebreakFix(s, fields),
		"search %s: Tiebreak (%s) covers no unique key of %s; %s", s.name, sqlNames(s.order), t.sqlName,
		keysOf(t))
}

// primaryKey returns the primary key of t, from the primary key tags of its
// model, in the index order of its UniqueKeys entry.
func (c *collector) primaryKey(s *searchDecl, t *tableBase) (uniqueKey, bool) {
	var pk []string
	for _, col := range t.columns {
		if col.primary {
			pk = append(pk, col.field)
		}
	}
	if len(pk) == 0 {
		fix := "add a primary key to " + t.sqlName + " in a migration, then run nano gen db"
		if k, ok := firstOrderingKey(t); ok {
			fix += ", or nanorepo.Tiebreak(" + columnList(s, k.fields) + ")"
		}
		c.errorf(s.tableNode, fix,
			"search %s: %s has no primary key, so no order is total: pages could skip or repeat rows",
			s.name, t.sqlName)
		return uniqueKey{}, false
	}
	for _, k := range t.keys {
		if len(k.fields) == len(pk) &&
			!slices.ContainsFunc(pk, func(f string) bool { return !slices.Contains(k.fields, f) }) {
			return k, true
		}
	}
	c.errorf(s.tableNode, "run nano gen db, which lists the primary key in UniqueKeys",
		"search %s: the primary key (%s) of %s, from its model, is not among its UniqueKeys", s.name,
		strings.Join(pk, ", "), t.sqlName)
	return uniqueKey{}, false
}

// ordersEveryRow returns "" when k orders every row of t, else why not.
func ordersEveryRow(t *tableBase, k uniqueKey) string {
	if k.pred != "" {
		return "it is partial"
	}
	for _, f := range k.fields {
		col, found := t.column(f)
		if !found {
			return "nano gen db must regenerate it"
		}
		if col.nullable {
			return col.sqlName + " can be NULL, and NULL rows repeat"
		}
	}
	return ""
}

// firstOrderingKey returns the first key of t that orders every row.
func firstOrderingKey(t *tableBase) (uniqueKey, bool) {
	for _, k := range t.keys {
		if ordersEveryRow(t, k) == "" {
			return k, true
		}
	}
	return uniqueKey{}, false
}

// tiebreakFix is the fix of E4: the default, or the missing columns of the
// first key that orders every row added to the list.
func (c *collector) tiebreakFix(s *searchDecl, fields []string) string {
	t := s.root.table
	k, ok := firstOrderingKey(t)
	if !ok {
		return "add a primary key to " + t.sqlName + " in a migration, then run nano gen db"
	}
	all := slices.Clone(fields)
	for _, f := range k.fields {
		if !slices.Contains(all, f) {
			all = append(all, f)
		}
	}
	fix := "add the missing columns: nanorepo.Tiebreak(" + columnList(s, all) + ")"
	if slices.ContainsFunc(t.columns, func(col column) bool { return col.primary }) {
		fix = "remove Tiebreak to use the primary key, or " + fix
	}
	return fix
}

// keysOf lists the keys of t for a message, with why a key does not count.
func keysOf(t *tableBase) string {
	if len(t.keys) == 0 {
		return t.sqlName + " has no unique key"
	}
	var keys, notCounted []string
	for _, k := range t.keys {
		if why := ordersEveryRow(t, k); why != "" {
			notCounted = append(notCounted, k.String()+" does not count: "+why)
			continue
		}
		keys = append(keys, k.String())
	}
	out := "it has no key that orders every row"
	if len(keys) > 0 {
		out = "its keys are " + strings.Join(keys, ", ")
	}
	if len(notCounted) > 0 {
		out += "; " + strings.Join(notCounted, "; ")
	}
	return out
}

// columnList renders the columns of fields as the declaration writes them:
// table.Items.Sku, table.Items.ID.
func columnList(s *searchDecl, fields []string) string {
	prefix := s.tableText
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = prefix + "." + f
	}
	return strings.Join(out, ", ")
}

// keyColumns returns the columns of k, in key order.
func keyColumns(t *tableBase, k uniqueKey) []column {
	out := make([]column, 0, len(k.fields))
	for _, f := range k.fields {
		col, _ := t.column(f)
		out = append(out, col)
	}
	return out
}

// sqlNames renders column names.
func sqlNames(cols []column) string {
	out := make([]string, len(cols))
	for i, col := range cols {
		out[i] = col.sqlName
	}
	return strings.Join(out, ", ")
}

// isPrimaryKey reports whether k has exactly the primary key columns of t.
func isPrimaryKey(t *tableBase, k uniqueKey) bool {
	n := 0
	for _, col := range t.columns {
		if col.primary {
			n++
			if !slices.Contains(k.fields, col.field) {
				return false
			}
		}
	}
	return n > 0 && n == len(k.fields)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// optionColumn is one column that an option names, with the option.
type optionColumn struct {
	option string // Scope, Filter, Sort, ...
	col    *colRef
}

// optionColumns returns every column that an option names on the read
// set: all but the child columns of FilterExists.
func (s *searchDecl) optionColumns() []optionColumn {
	var out []optionColumn
	for _, sc := range s.scopes {
		for _, col := range sc.cols {
			out = append(out, optionColumn{sc.kind, col})
		}
	}
	for _, sf := range s.scopeFuncs {
		out = append(out, optionColumn{"ScopeFunc", sf.col})
	}
	for _, u := range s.unscoped {
		out = append(out, optionColumn{"Unscoped", u.col})
	}
	for _, f := range s.filters {
		if f.col != nil && f.kind != "FilterExists" {
			out = append(out, optionColumn{f.kind, f.col})
		}
	}
	for _, col := range s.fullText {
		out = append(out, optionColumn{"FullText", col})
	}
	for _, sort := range s.sorts {
		if sort.col != nil {
			out = append(out, optionColumn{sort.kind, sort.col})
		}
	}
	for _, col := range s.tiebreak {
		out = append(out, optionColumn{"Tiebreak", col})
	}
	if s.defaultSort != nil && s.defaultSort.col != nil {
		out = append(out, optionColumn{"DefaultSort", s.defaultSort.col})
	}
	return out
}

// checkColumnsRead reports each column of an option on an instance that the
// search does not read (E5), and a FilterExists column that is not a column
// of its child table.
func (c *collector) checkColumnsRead(s *searchDecl) bool {
	ok := true
	for _, oc := range s.optionColumns() {
		if s.instanceOf(oc.col) != nil {
			continue
		}
		ok = false
		inst := &instance{table: oc.col.table, alias: oc.col.alias, text: oc.col.tableText}
		names := make([]string, len(s.reads))
		for i, read := range s.reads {
			names[i] = read.String()
		}
		c.errorf(oc.col.expr, c.joinFix(s, inst), "search %s: %s(%s) names %s, which search %s does not read "+
			"(it reads %s)", s.name, oc.option, oc.col, inst, s.name, strings.Join(names, ", "))
	}
	for _, f := range s.filters {
		if f.kind != "FilterExists" {
			continue
		}
		for _, col := range []*colRef{f.link, f.col} {
			if col.table != f.child || col.alias != "" {
				ok = false
				c.errorf(col.expr, "name a column of "+f.child.sqlName+", the child table of FilterExists",
					"search %s: FilterExists(%s, ...) names %s, which is not a column of %s", s.name,
					f.child.sqlName, col, f.child.sqlName)
			}
		}
	}
	return ok
}
