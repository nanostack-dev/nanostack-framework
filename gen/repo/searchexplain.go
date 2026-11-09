package genrepo

import (
	"fmt"
	"go/ast"
	"io"
	"slices"
	"strings"
)

// SearchExplanation is what nano gen repo read from one nanorepo.Search, as
// -explain prints it: the table instances, the scopes, the filters, the
// sorts and the order.
type SearchExplanation struct {
	Name  string
	Table string
	Row   string // the row type, as the declaring package writes it
	Lines []ExplainedLine
}

// ExplainedLine is one labelled line of a SearchExplanation.
type ExplainedLine struct {
	Label string // read, scope, filter, sort, default, tiebreak, ...
	Text  string
}

// explainSearches explains every search of d.
func (l *loaded) explainSearches(d *declarations) []SearchExplanation {
	c := &collector{l: l, d: d}
	out := make([]SearchExplanation, 0, len(d.searches))
	for _, s := range d.searches {
		out = append(out, c.explainSearch(s))
	}
	return out
}

func (c *collector) explainSearch(s *searchDecl) SearchExplanation {
	e := SearchExplanation{Name: s.name, Table: s.root.table.sqlName, Row: c.qualified(s.row)}
	add := func(label, format string, args ...any) {
		e.Lines = append(e.Lines, ExplainedLine{Label: label, Text: fmt.Sprintf(format, args...)})
	}
	if s.from != nil {
		add("from", "%s", s.from.name)
	}
	for _, inst := range s.reads {
		add("read", "%s", c.explainInstance(s, inst))
	}
	for _, in := range s.inner {
		text := in.role + " reads " + in.inst.String()
		if in.inst.join != "" {
			text += "; " + in.inst.join + "; ON " + c.source(in.inst.on)
		}
		if in.where != nil {
			text += "; WHERE " + c.source(in.where)
		}
		add("subquery", "%s", text)
	}
	if s.mapper != nil {
		kind := "Map"
		if s.mapper.page {
			kind = "MapPage"
		}
		add("map", "%s(%s)", kind, c.source(s.mapper.fn))
	}
	c.explainScopes(s, add)
	for _, f := range s.filters {
		add("filter", "%s", c.explainFilter(s, f))
	}
	goOnly := func(e ast.Expr) string {
		if s.fullTextGoOnly[e] {
			return ", Go only"
		}
		return ""
	}
	for _, col := range s.fullText {
		add("fulltext", "%s (%s)%s", c.qualifiedColumn(s, col), col.table.catalog[col.col.sqlName],
			goOnly(col.expr))
	}
	for _, fn := range s.fullTextExprs {
		add("fulltext", "%s()%s", c.source(fn), goOnly(fn))
	}
	for _, sort := range s.sorts {
		add("sort", "%s", c.explainSort(s, sort))
	}
	if d := s.defaultSort; d != nil {
		dir := "ASC"
		if d.desc {
			dir = "DESC"
		}
		if d.col != nil {
			add("default", "%s %s", c.qualifiedColumn(s, d.col), dir)
		} else {
			add("default", "%s %s", d.field, dir)
		}
	}
	how := "covers " + s.orderKey.String()
	if s.orderDefault {
		how = "the primary key " + s.orderKey.String() + ", by default"
	}
	add("tiebreak", "%s: %s", sqlNames(s.order), how)
	if s.defaultLimit > 0 {
		add("limit", "%d", s.defaultLimit)
	}
	c.explainAPI(s, add)
	return e
}

// explainInstance renders one instance: its join, the nullable columns and
// the tenant columns of Postgres.Scopes.
func (c *collector) explainInstance(s *searchDecl, inst *instance) string {
	parts := []string{inst.String()}
	if inst == s.root {
		parts = append(parts, "root")
	}
	if inst.join != "" {
		parts = append(parts, inst.join)
	}
	if inst.on != nil {
		parts = append(parts, "ON "+c.source(inst.on))
	}
	if inst.key != nil {
		parts = append(parts, "one row by "+inst.key.String())
	}
	if inst.nullable {
		parts = append(parts, "every column nullable")
	} else if nullable := nullableColumns(inst.table); nullable != "" {
		parts = append(parts, "nullable "+nullable)
	}
	var tenant []string
	for _, col := range inst.table.columns {
		if slices.Contains(c.l.scopes, col.sqlName) {
			tenant = append(tenant, col.sqlName)
		}
	}
	if len(tenant) > 0 {
		parts = append(parts, "tenant "+strings.Join(tenant, ", "))
	}
	return strings.Join(parts, "; ")
}

func nullableColumns(t *tableBase) string {
	var out []string
	for _, col := range t.columns {
		if col.nullable {
			out = append(out, col.sqlName)
		}
	}
	return strings.Join(out, ", ")
}

// explainScopes adds the scope, scope function and Unscoped lines.
func (c *collector) explainScopes(s *searchDecl, add func(label, format string, args ...any)) {
	for _, sc := range s.scopes {
		names := make([]string, len(sc.cols))
		for i, col := range sc.cols {
			names[i] = c.qualifiedColumn(s, col)
		}
		add("scope", "%s %s", sc.kind, strings.Join(names, ", "))
	}
	for _, sf := range s.scopeFuncs {
		add("scope", "ScopeFunc %s on %s", c.source(sf.fn), c.qualifiedColumn(s, sf.col))
	}
	for _, u := range s.unscoped {
		add("scope", "Unscoped %s: %s", c.qualifiedColumn(s, u.col), u.why)
	}
	for _, fn := range s.conditions {
		add("condition", "%s()", c.source(fn))
	}
}

// explainFilter renders a filter: its field, its operators and what it
// reads.
func (c *collector) explainFilter(s *searchDecl, f *searchFilter) string {
	ops := make([]string, len(f.ops))
	for i, op := range f.ops {
		ops[i] = op.name
		if op.bounds != "" {
			ops[i] += "." + op.bounds
		}
		if len(op.api) > 0 {
			ops[i] += " (" + strings.Join(op.api, ", ") + ")"
		}
	}
	if f.kind == "Flag" {
		ops = []string{"Is"}
	}
	text := f.name + ": " + strings.Join(ops, ", ")
	switch {
	case f.kind == "FilterExists":
		text += " on EXISTS " + f.child.sqlName + " by " + c.qualifiedColumn(s, f.link)
	case f.col != nil:
		value := c.qualified(f.col.col.value)
		if f.as != nil {
			value = c.qualified(f.as)
		}
		text += " on " + c.qualifiedColumn(s, f.col) + " (" + f.col.table.catalog[f.col.col.sqlName] +
			", " + value + ")"
	default:
		text += " on " + c.source(f.expr) + "()"
	}
	if f.goOnly {
		text += ", Go only"
	}
	return text
}

// explainSort renders a sort: its sort_by values and what it orders by.
func (c *collector) explainSort(s *searchDecl, sort *searchSort) string {
	text := strings.Join(sort.api, ", ") + ": "
	switch sort.kind {
	case "SortExpr":
		text += "field " + sort.field + " = " + c.source(sort.expr) + "()"
	case "SortHandWritten":
		text += "hand-written"
	default:
		text += c.qualifiedColumn(s, sort.col)
		if inst := s.instanceOf(sort.col); inst != nil && inst.columnNullable(sort.col.col) {
			text += ", nullable"
		}
		if sort.nulls != "" {
			text += ", " + sort.nulls
		}
	}
	return text
}

// explainAPI adds the API lines.
func (c *collector) explainAPI(s *searchDecl, add func(label, format string, args ...any)) {
	if s.api != nil {
		add("api", "%s", c.qualified(s.api))
	}
	if s.ignoresFullText != "" {
		add("api", "IgnoresFullText: %s", s.ignoresFullText)
	}
	if len(s.apiHandWritten) > 0 {
		add("api", "APIHandWritten %s", strings.Join(s.apiHandWritten, ", "))
	}
}

// qualifiedColumn renders a column as table.column, or alias.column.
func (c *collector) qualifiedColumn(s *searchDecl, col *colRef) string {
	if inst := s.instanceOf(col); inst != nil {
		return inst.name() + "." + col.col.sqlName
	}
	return (&instance{table: col.table, alias: col.alias}).name() + "." + col.col.sqlName
}

// instanceOf returns the instance that col names, or nil when the search
// does not read it (E5).
func (s *searchDecl) instanceOf(col *colRef) *instance {
	for _, inst := range s.reads {
		if inst.is(col.table, col.alias) {
			return inst
		}
	}
	return nil
}

// WriteSearchExplain writes the explanations of searches as nano gen repo
// -explain prints them: per search a header line, then one labelled line
// per fact, with the labels of every search in one column.
func WriteSearchExplain(w io.Writer, explain []SearchExplanation) {
	width := 0
	for _, e := range explain {
		for _, line := range e.Lines {
			width = max(width, len(line.Label))
		}
	}
	for _, e := range explain {
		_, _ = fmt.Fprintf(w, "search %s on %s (rows %s)\n", e.Name, e.Table, e.Row)
		for _, line := range e.Lines {
			_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, line.Label, line.Text)
		}
	}
}
