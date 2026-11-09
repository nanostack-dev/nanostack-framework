package genrepo

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
)

// rowPlan is what the Row of a search selects (design S9.1), and the Row
// field that reads each selected column. A key reads its value from that
// field.
type rowPlan struct {
	model bool      // the Row is the model of the searched table: it selects its AllColumns
	projs []rowProj // the projection, in the order of the Row fields
	cols  map[instCol]*rowField
}

// rowProj is one entry of the projection: every column of an instance, or
// one column.
type rowProj struct {
	inst *instance
	col  *column // nil: the AllColumns of inst
}

// instCol names one column of one instance.
type instCol struct {
	inst *instance
	col  string // the column name
}

// rowField is the Row field that reads one column.
type rowField struct {
	path string     // the selector after r, such as Items.Name or TenantName
	typ  types.Type // the type of the field
	// bad is true when the field has a problem that is reported already: a
	// key on its column reports nothing more.
	bad bool
}

// builderRow works out the projection of the Row (design S9.1):
//   - the model of the searched table selects its AllColumns;
//   - an embedded model of a table the search reads selects the AllColumns
//     of that instance (an alias:"owner" tag names an aliased instance);
//   - a field tagged alias:"table.column" selects that column;
//   - any other field is selected by no projection, which is allowed only
//     when a SortHandWritten key of the hand-written method selects it.
//
// A field that reads a column that can be NULL in a result row (a pointer
// in the model, or the nullable side of a LEFT JOIN) must be able to hold
// NULL (E26). Every other problem of the Row is E25.
func (c *collector) builderRow(b *builder) {
	s := b.s
	rp := &rowPlan{cols: map[instCol]*rowField{}}
	b.row = rp
	if model := s.root.table.model; model != nil && types.Identical(s.row, model) {
		rp.model = true
		rp.projs = []rowProj{{inst: s.root}}
		for _, col := range s.root.table.columns {
			typ := modelField(model, col.field)
			rp.cols[instCol{s.root, col.sqlName}] = &rowField{path: col.field, typ: typ}
		}
		return
	}
	st, isStruct := types.Unalias(s.row).Underlying().(*types.Struct)
	if !isStruct {
		c.errorf(s.tableNode, "search a struct, such as the model of "+s.root.table.sqlName,
			"search %s: the Row %s is not a struct", s.name, c.qualified(s.row))
		return
	}
	before := len(c.problems)
	handWritten := slices.ContainsFunc(s.sorts, func(x *searchSort) bool { return x.kind == "SortHandWritten" })
	for i := range st.NumFields() {
		f := st.Field(i)
		tag, tagged := reflect.StructTag(st.Tag(i)).Lookup("alias")
		switch {
		case f.Embedded():
			c.rowEmbedded(b, f, tag)
		case tagged:
			c.rowAlias(b, f, tag)
		case !f.Exported():
			c.rowError(b, f, "export it and tag it with the column it reads, or remove it",
				"%s is not exported, and the scan sets only exported fields", c.rowFieldName(b, f))
		case slices.ContainsFunc(s.sorts, func(x *searchSort) bool { return x.kind == "SortExpr" && x.field == f.Name() }):
			// the projection of its SortExpr key selects it
		case !handWritten:
			c.rowError(b, f, c.plainFieldFix(b, f),
				"%s has no alias tag, so the search selects nothing for it", c.rowFieldName(b, f))
		}
	}
	rp.projs = dedupe(rp.projs)
	if len(rp.projs) == 0 && len(c.problems) == before {
		c.errorf(s.tableNode, "embed the model of "+s.root.table.sqlName+" in "+c.qualified(s.row)+
			", or tag its fields with alias:\"table.column\"",
			"search %s: the Row %s selects no column", s.name, c.qualified(s.row))
	}
}

// rowEmbedded reads an embedded field of the Row: the model of a table
// that the search reads.
func (c *collector) rowEmbedded(b *builder, f *types.Var, tag string) {
	s := b.s
	name := c.rowFieldName(b, f)
	if p, isPtr := types.Unalias(f.Type()).(*types.Pointer); isPtr && c.modelInstance(s, p.Elem()) != nil {
		c.rowError(b, f, "embed "+c.qualified(p.Elem())+", or read a nullable instance with alias-tagged pointer "+
			"fields", "%s embeds a pointer to a model, and nano gen repo reads only an embedded model", name)
		return
	}
	var inst *instance
	for _, r := range s.reads {
		if r.table.model != nil && types.Identical(r.table.model, f.Type()) && r.alias == tag {
			inst = r
			break
		}
	}
	if inst == nil {
		c.rowEmbeddedNoInstance(b, f, tag)
		return
	}
	var notNull []string
	for _, col := range inst.table.columns {
		typ := modelField(f.Type(), col.field)
		rf := &rowField{path: f.Name() + "." + col.field, typ: typ}
		if inst.nullable && !holdsNull(typ) {
			notNull, rf.bad = append(notNull, col.field), true
		}
		key := instCol{inst, col.sqlName}
		if _, mapped := b.row.cols[key]; !mapped {
			b.row.cols[key] = rf
		}
	}
	b.row.projs = append(b.row.projs, rowProj{inst: inst})
	if len(notNull) > 0 {
		col := inst.table.columns[0]
		for _, c := range inst.table.columns {
			if c.field == notNull[0] {
				col = c
			}
		}
		c.rowError(b, f, "replace the embedded "+c.qualified(f.Type())+" with alias-tagged pointer fields, such as "+
			c.suggestField(b, inst, col),
			"%s embeds %s, whose fields %s read %s, which is NULL when no %s row matches (LEFT JOIN)",
			name, c.qualified(f.Type()), strings.Join(notNull, ", "), inst.name(), inst.name())
	}
}

// rowEmbeddedNoInstance reports an embedded field that no instance of the
// search matches.
func (c *collector) rowEmbeddedNoInstance(b *builder, f *types.Var, tag string) {
	s := b.s
	name := c.rowFieldName(b, f)
	var aliased []string
	for _, r := range s.reads {
		if r.table.model != nil && types.Identical(r.table.model, f.Type()) {
			aliased = append(aliased, r.name())
		}
	}
	switch {
	case len(aliased) > 0 && tag == "":
		c.rowError(b, f, "tag the field alias:\""+aliased[0]+"\"",
			"%s embeds %s, and the search reads %s only as %s: the embedded field names the instance with an "+
				"alias tag", name, c.qualified(f.Type()), tableOf(s, f.Type()),
			strings.Join(aliased, ", "))
	case len(aliased) > 0:
		c.rowError(b, f, "tag the field alias:\""+aliased[0]+"\"",
			"%s embeds %s with alias:%q, and the search reads no instance %s; it reads %s", name,
			c.qualified(f.Type()), tag, tag, strings.Join(aliased, ", "))
	default:
		c.rowError(b, f, "remove it, or join its table in the From function",
			"%s embeds %s, which is not the model of a table the search reads (%s)", name,
			c.qualified(f.Type()), instanceNames(s))
	}
}

// rowAlias reads a field tagged alias:"table.column".
func (c *collector) rowAlias(b *builder, f *types.Var, tag string) {
	s := b.s
	name := c.rowFieldName(b, f)
	if !f.Exported() {
		c.rowError(b, f, "export it: rename it "+strings.ToUpper(f.Name()[:1])+f.Name()[1:],
			"%s is not exported, and the scan sets only exported fields", name)
		return
	}
	table, colName, _ := strings.Cut(tag, ".")
	var inst *instance
	for _, r := range s.reads {
		if r.name() == table {
			inst = r
		}
	}
	var col *column
	if inst != nil {
		for i := range inst.table.columns {
			if inst.table.columns[i].sqlName == colName {
				col = &inst.table.columns[i]
			}
		}
	}
	if col == nil {
		c.rowError(b, f, c.aliasFix(s, tag),
			"%s has alias:%q, which names no column of a table the search reads (%s)", name, tag,
			instanceNames(s))
		return
	}
	elem, _ := optional(f.Type())
	if !types.Identical(elem.Underlying(), col.value.Underlying()) {
		c.rowError(b, f, "make it "+c.fieldType(*col, holdsNull(f.Type())),
			"%s is %s, and %s.%s is %s in the model", name, c.qualified(f.Type()), inst.name(), col.sqlName,
			c.qualified(col.value))
		return
	}
	rf := &rowField{path: f.Name(), typ: f.Type()}
	if inst.columnNullable(*col) && !holdsNull(f.Type()) {
		rf.bad = true
		why := "which can be NULL (the model field is a pointer)"
		if inst.nullable {
			why = "which is NULL when no " + inst.name() + " row matches (LEFT JOIN)"
		}
		c.rowError(b, f, "make it "+c.fieldType(*col, true), "%s reads %s.%s, %s", name, inst.name(),
			col.sqlName, why)
	}
	key := instCol{inst, col.sqlName}
	if _, mapped := b.row.cols[key]; !mapped {
		b.row.cols[key] = rf
	}
	b.row.projs = append(b.row.projs, rowProj{inst: inst, col: col})
}

// dedupe drops a column that an AllColumns of its instance, or an earlier
// entry, already selects: one column under one alias fills every Row field
// that names it.
func dedupe(projs []rowProj) []rowProj {
	all := map[*instance]bool{}
	for _, p := range projs {
		if p.col == nil {
			all[p.inst] = true
		}
	}
	seen := map[instCol]bool{}
	out := projs[:0:0]
	for _, p := range projs {
		if p.col != nil {
			key := instCol{p.inst, p.col.sqlName}
			if all[p.inst] || seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, p)
	}
	return out
}

// holdsNull reports whether a field of type t can hold NULL: a pointer or a
// functional.Option.
func holdsNull(t types.Type) bool {
	if t == nil {
		return false
	}
	_, nullable := optional(t)
	return nullable
}

// rowError reports a problem of the Row field f at the field when the
// declaring package declares it, else at the search.
func (c *collector) rowError(b *builder, f *types.Var, fix, format string, args ...any) {
	where := ast.Node(b.s.tableNode)
	if f.Pkg() == c.l.pkg.Types && f.Pos().IsValid() {
		where = posNode(f.Pos())
	}
	c.errorf(where, fix, "search "+b.s.name+": "+format, args...)
}

// posNode is a node at one position, for a problem whose place is an
// object, not a node.
type posNode token.Pos

func (p posNode) Pos() token.Pos { return token.Pos(p) }
func (p posNode) End() token.Pos { return token.Pos(p) }

// rowFieldName is a Row field as a message names it: itemRow.TenantName.
func (c *collector) rowFieldName(b *builder, f *types.Var) string {
	return c.qualified(b.s.row) + "." + f.Name()
}

// plainFieldFix is the fix of a Row field with no alias tag.
func (c *collector) plainFieldFix(b *builder, f *types.Var) string {
	elem, _ := optional(f.Type())
	for _, inst := range b.s.reads {
		for _, col := range inst.table.columns {
			if col.field == f.Name() && types.Identical(elem.Underlying(), col.value.Underlying()) {
				return "tag it `alias:\"" + inst.name() + "." + col.sqlName + "\"`"
			}
		}
	}
	return "tag it with the column it reads, such as `alias:\"" + b.s.root.name() + "." +
		b.s.root.table.columns[0].sqlName + "\"`, or remove it: only the key of a SortHandWritten sort fills a " +
		"field with no tag"
}

// aliasFix is the fix of an alias tag that names no column: the column of
// the same name in another spelling, else the form of a tag.
func (c *collector) aliasFix(s *searchDecl, tag string) string {
	for _, inst := range s.reads {
		for _, col := range inst.table.columns {
			if want := inst.name() + "." + col.sqlName; commonIdent(want) == commonIdent(tag) {
				return "write alias:\"" + want + "\", as SQL names the column"
			}
		}
	}
	return "write alias:\"<table or alias>.<column>\" with a table the search reads, such as alias:\"" +
		s.root.name() + "." + s.root.table.columns[0].sqlName + "\""
}

// commonIdent is a name as the scan compares it: lower case, without _ or -.
func commonIdent(name string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(name))
}

// fieldType renders the Go type a Row field needs for col.
func (c *collector) fieldType(col column, nullable bool) string {
	t := c.qualified(col.value)
	if nullable {
		return "*" + t
	}
	return t
}

// suggestField renders a Row field that reads col of inst, for a fix.
func (c *collector) suggestField(b *builder, inst *instance, col column) string {
	name := col.field
	if inst != b.s.root {
		name = goName(singular(inst.name())) + col.field
	}
	return name + " " + c.fieldType(col, inst.columnNullable(col)) + " `alias:\"" + inst.name() + "." +
		col.sqlName + "\"`"
}

// singular drops the plural of a table name: collections -> collection,
// categories -> category.
func singular(name string) string {
	switch {
	case strings.HasSuffix(name, "ies") && len(name) > 3:
		return strings.TrimSuffix(name, "ies") + "y"
	case strings.HasSuffix(name, "ss"):
		return name
	case strings.HasSuffix(name, "s") && len(name) > 1:
		return strings.TrimSuffix(name, "s")
	}
	return name
}

// instanceNames lists the instances of s for a message.
func instanceNames(s *searchDecl) string {
	names := make([]string, len(s.reads))
	for i, inst := range s.reads {
		names[i] = inst.name()
	}
	return strings.Join(names, ", ")
}

// tableOf returns the table name of the model t among the instances of s.
func tableOf(s *searchDecl, t types.Type) string {
	for _, inst := range s.reads {
		if inst.table.model != nil && types.Identical(inst.table.model, t) {
			return inst.table.sqlName
		}
	}
	return ""
}

// modelInstance returns an instance of s whose model is t, or nil.
func (c *collector) modelInstance(s *searchDecl, t types.Type) *instance {
	for _, inst := range s.reads {
		if inst.table.model != nil && types.Identical(inst.table.model, t) {
			return inst
		}
	}
	return nil
}
