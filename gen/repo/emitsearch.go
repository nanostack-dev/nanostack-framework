package genrepo

import (
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

const keysetPath = dbPath + "/keyset"

// searchMarker starts the code of one search in <file>_repo.gen.go: the
// first line of the doc comment of its builder type. A type error on a
// later line is the search's, and the line count test reads from it.
func searchMarker(name string) string { return "// " + name + "Search is an immutable search of " }

func (e *emitter) sx(ident string) string { return e.ref(searchexecPath, "searchexec", ident) }
func (e *emitter) ks(ident string) string { return e.ref(keysetPath, "keyset", ident) }

// column renders a column as the declaration names it: table.Items.Name,
// or table.Tenants.AS("owner").Name.
func (e *emitter) column(col *colRef) string {
	return e.instance(col.table, col.alias) + "." + col.col.field
}

// instance renders a table instance: table.Items, or
// table.Tenants.AS("owner").
func (e *emitter) instance(t *tableBase, alias string) string {
	ref := e.ref(t.pkgPath, t.pkgName, t.goName)
	if alias != "" {
		ref += ".AS(" + strconv.Quote(alias) + ")"
	}
	return ref
}

// columnsWidth is the width up to which the projection of a Row of its
// own takes one line.
const columnsWidth = 100

// columns writes the projection of the spec (design S9.1): the AllColumns
// of the searched table when the Row is its model, else one entry per
// embedded model or alias-tagged field of the Row.
func (e *emitter) columns(b *builder, row string) {
	projs := b.row.projs
	if len(projs) == 1 && projs[0].col == nil {
		e.p("\tColumns: %s.AllColumns, // the Row is %s\n", e.instance(projs[0].inst.table, projs[0].inst.alias), row)
		return
	}
	parts := make([]string, len(projs))
	for i, p := range projs {
		if p.col == nil {
			parts[i] = e.instance(p.inst.table, p.inst.alias) + ".AllColumns"
			continue
		}
		parts[i] = e.column(&colRef{table: p.inst.table, alias: p.inst.alias, col: *p.col})
	}
	head := "\tColumns: " + e.pg("ProjectionList") + "{"
	if one := head + strings.Join(parts, ", ") + "},"; len(one) <= columnsWidth {
		e.p("%s // the Row is %s\n", one, row)
		return
	}
	e.p("%s // the Row is %s\n", head, row)
	line := ""
	for _, part := range parts {
		if line != "" && len(line)+len(part)+3 > columnsWidth {
			e.p("\t\t%s\n", line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += part + ","
	}
	e.p("\t\t%s\n\t},\n", line)
}

// fn renders a function of the declaring package, or of an import.
func (e *emitter) fn(obj types.Object) string {
	if obj.Pkg() == nil || obj.Pkg().Path() == e.self {
		return obj.Name()
	}
	return e.ref(obj.Pkg().Path(), obj.Pkg().Name(), obj.Name())
}

// scopeType renders the scope value P of a builder.
func (e *emitter) scopeType(b *builder) string {
	switch len(b.fields) {
	case 0:
		return "struct{}"
	case 1:
		return e.typ(b.fields[0].typ)
	}
	return b.scopeName()
}

// searchBuilder writes the builder of one search (design 4.2): its type,
// its scope type, its sort enum, its spec, its constructor, one accessor per
// filter, and FullText.
func (e *emitter) searchBuilder(b *builder) {
	s := b.s
	q, sortT, row, p := s.name+"Search", s.name+"Sort", e.typ(s.row), e.scopeType(b)
	e.p("%s%s. Start one with New%s.\n", searchMarker(s.name), s.root.table.sqlName, q)
	e.p("type %s struct {\n\t%s[%s, %s, %s, %s]\n}\n\n", q, e.sx("Query"), row, q, sortT, p)
	e.scopeStruct(b)
	e.p("// %s is a sort of %s.\ntype %s string\n\n", sortT, q, sortT)
	if len(b.sorts) > 0 {
		e.p("const (\n")
		for _, sort := range b.sorts {
			e.p("\t%s%s %s = %s\n", sortT, sort.name, sortT, strconv.Quote(sort.api))
		}
		e.p(")\n\n")
	}
	e.spec(b, row, sortT, p)
	e.constructor(b, q, p)
	e.accessors(b, q)
}

// scopeStruct writes the scope type of a search with two or more scope
// fields. Its blank first field makes an unkeyed literal fail to compile in
// another package (design S4).
func (e *emitter) scopeStruct(b *builder) {
	if len(b.fields) < 2 {
		return
	}
	e.p("// %s is the tenant of %s %sSearch. A zero field refuses the search.\n", b.scopeName(), article(b.s.name),
		b.s.name)
	e.p("type %s struct {\n\t_ struct{} // keyed fields only\n", b.scopeName())
	for _, f := range b.fields {
		cols := make([]string, len(f.cols))
		for i, col := range f.cols {
			cols[i] = (&instance{table: col.table, alias: col.alias}).name() + "." + col.col.sqlName
		}
		note := strings.Join(cols, ", ")
		if f.kind == "ScopeOrNull" {
			note += ", or NULL"
		}
		e.p("\t%s %s // %s\n", f.name, e.typ(f.typ), note)
	}
	e.p("}\n\n")
}

// spec writes the spec variable: searchexec.DefineSpec checks it when the
// package starts.
func (e *emitter) spec(b *builder, row, sortT, p string) {
	s := b.s
	e.p("var %s = %s(%s[%s, %s, %s]{\n", specVar(s), e.sx("DefineSpec"), e.sx("SpecDef"), row, sortT, p)
	from := e.ref(s.root.table.pkgPath, s.root.table.pkgName, s.root.table.goName)
	switch {
	case b.from != nil && s.from.takesScope:
		e.p("\tFrom: func(s %s) %s { return %s(s) },\n", p, e.ks("Source"), e.fn(b.from))
	case b.from != nil:
		e.p("\tFrom: func(%s) %s { return %s() },\n", p, e.ks("Source"), e.fn(b.from))
	default:
		e.p("\tFrom: func(%s) %s { return %s },\n", p, e.ks("Source"), from)
	}
	e.columns(b, row)
	e.scopes(b, p)
	e.context(b)
	e.p("\tKeys: map[%s]%s[%s]{\n", sortT, e.ks("Key"), row)
	for _, sort := range b.sorts {
		if sort.key != nil {
			e.p("\t\t%s%s: %s,\n", sortT, sort.name, e.key(sort.key, row))
		}
	}
	note := s.orderKey.String()
	if !s.orderDefault {
		cols := make([]string, len(s.order))
		for i, col := range s.order {
			cols[i] = col.sqlName
		}
		note = strings.Join(cols, ", ") + ": covers " + note
	}
	e.p("\t},\n\tTiebreak: []%s[%s]{ // %s\n", e.ks("Key"), row, note)
	for _, k := range b.tiebreak {
		e.p("\t\t%s,\n", e.key(k, row))
	}
	e.p("\t},\n")
	if b.dflt != nil {
		dir := "Asc"
		if s.defaultSort.desc {
			dir = "Desc"
		}
		e.p("\tDefault: %s(%s%s),\n", e.sx(dir), sortT, b.dflt.name)
	}
	limit := s.defaultLimit
	if limit == 0 {
		limit = defaultLimit
	}
	e.p("\tLimit: %d,\n})\n\n", limit)
}

// context writes the context scopes, the Unscoped record and the
// conditions of a spec.
func (e *emitter) context(b *builder) {
	s := b.s
	if len(b.ctx) > 0 {
		parts := make([]string, len(b.ctx))
		for i, c := range b.ctx {
			parts[i] = e.sx("ScopeFunc") + "(" + e.fn(c.fn) + ", " + e.column(c.col) + ")"
		}
		e.p("\tContext: []%s{%s},\n", e.sx("ContextScope"), strings.Join(parts, ", "))
	}
	if len(s.unscoped) > 0 {
		parts := make([]string, len(s.unscoped))
		for i, u := range s.unscoped {
			name := (&instance{table: u.col.table, alias: u.col.alias}).name()
			parts[i] = strconv.Quote(name + "." + u.col.col.sqlName + ": " + u.why)
		}
		e.p("\tUnscoped: []string{%s},\n", strings.Join(parts, ", "))
	}
	if len(b.conds) > 0 {
		parts := make([]string, len(b.conds))
		for i, fn := range b.conds {
			parts[i] = e.fn(fn) + "()"
		}
		e.p("\tConditions: []%s{%s},\n", e.pg("BoolExpression"), strings.Join(parts, ", "))
	}
}

// scopes writes the tenant conditions that read the scope value.
func (e *emitter) scopes(b *builder, p string) {
	var parts []string
	field := 0
	for _, sc := range b.s.scopes {
		cols := make([]string, len(sc.cols))
		for i, col := range sc.cols {
			cols[i] = e.column(col)
		}
		if sc.kind == "ScopeNull" {
			parts = append(parts, e.sx("ScopeNull")+"["+p+"]("+cols[0]+")")
			continue
		}
		f := b.fields[field]
		field++
		value := e.sx("Value") + "[" + p + "]"
		if len(b.fields) > 1 {
			value = "func(s " + p + ") " + e.typ(f.typ) + " { return s." + f.name + " }"
		}
		ctor := "ScopeEq"
		if sc.kind == "ScopeOrNull" {
			ctor = "ScopeOrNull"
		}
		parts = append(parts, e.sx(ctor)+"("+value+", "+strings.Join(cols, ", ")+")")
	}
	if len(parts) == 0 {
		return
	}
	e.p("\tScope: []%s[%s]{\n", e.sx("ScopeCol"), p)
	for _, part := range parts {
		e.p("\t\t%s,\n", part)
	}
	e.p("\t},\n")
}

// key renders a keyset key and the accessor of its value.
func (e *emitter) key(k *sortKey, row string) string {
	args := e.column(k.col)
	if k.enumType != "" {
		labels := make([]string, len(k.labels))
		for i, label := range k.labels {
			labels[i] = "string(" + e.enumLabel(k.col, label) + ")"
		}
		args += ", " + strconv.Quote(k.enumType) + ", []string{" + strings.Join(labels, ", ") + "}"
	}
	body := "return r." + k.field
	switch {
	case k.conv != "":
		body = "return " + k.conv + "(r." + k.field + ")"
	case k.ptrConv != "":
		body = "if r." + k.field + " == nil {\n\t\treturn nil\n\t}\n\tv := " + k.ptrConv + "(*r." + k.field +
			")\n\treturn &v"
	}
	out := e.ks(k.ctor) + "(" + args + ", func(r " + row + ") " + e.typ(k.value) + " { " + body + " })"
	if k.nulls != "" {
		out += "." + k.nulls + "()"
	}
	return out
}

// enumLabel renders a model constant pkg.Name with the import name of the
// model package.
func (e *emitter) enumLabel(col *colRef, label string) string {
	named, _ := types.Unalias(col.col.value).(*types.Named)
	_, name, _ := strings.Cut(label, ".")
	return e.ref(named.Obj().Pkg().Path(), named.Obj().Pkg().Name(), name)
}

// constructor writes New<Name>Search, the only way to a query that runs.
func (e *emitter) constructor(b *builder, q, p string) {
	s := b.s
	e.p("// New%s starts %s %s of one tenant. Only a query built here runs: Resolve and Run compare the spec.\n",
		q, article(q), q)
	switch len(b.fields) {
	case 0:
		e.p("func New%s() %s { return %s[%s](%s, struct{}{}) }\n\n", q, q, e.sx("New"), q, specVar(s))
	case 1:
		v := lowerFirst(b.fields[0].name)
		if token.IsKeyword(v) {
			v += "Value"
		}
		e.p("func New%s(%s %s) %s { return %s[%s](%s, %s) }\n\n", q, v, p, q, e.sx("New"), q, specVar(s), v)
	default:
		e.p("func New%s(scope %s) %s { return %s[%s](%s, scope) }\n\n", q, p, q, e.sx("New"), q, specVar(s))
	}
}

// accessors writes one accessor per filter, then FullText.
func (e *emitter) accessors(b *builder, q string) {
	if len(b.filters) > 0 {
		e.p("// The filters of %s: each returns a field of q, whose method returns the filtered copy.\n", q)
	}
	for _, f := range b.filters {
		target := ""
		if f.f.col != nil {
			target = e.column(f.f.col)
		} else {
			target = e.fn(f.fn) + "()"
		}
		var typ, call string
		switch f.kind {
		case "In", "Eq", "EqIn":
			typ = e.sx(f.kind) + "[" + q + ", " + e.typ(f.value) + "]"
			call = e.sx("New"+f.kind) + "(q.Query, " + target + ", " + e.literal(f) + ")"
		case "InFold":
			typ = e.sx(f.kind) + "[" + q + ", " + e.typ(f.value) + "]"
			call = e.sx("NewInFold") + "[" + e.typ(f.value) + "](q.Query, " + target + ")"
		default:
			typ = e.sx(f.kind) + "[" + q + "]"
			call = e.sx("New"+f.kind) + "(q.Query, " + target + ")"
		}
		e.p("func (q %s) %s() %s { return %s }\n", q, f.f.name, typ, call)
	}
	if len(b.filters) > 0 {
		e.p("\n")
	}
	if len(b.fullText) == 0 {
		return
	}
	parts := make([]string, len(b.fullText))
	names := make([]string, len(b.fullText))
	for i, part := range b.fullText {
		if part.col != nil {
			parts[i], names[i] = e.column(part.col), part.col.col.sqlName
			continue
		}
		parts[i], names[i] = e.fn(part.fn)+"()", part.fn.Name()
	}
	e.p("// FullText keeps rows where %s contains term (searchexec.Contains). A blank term filters nothing.\n",
		orList(names))
	e.p("func (q %s) FullText(term string) %s { return %s(q.Query, %s).Contains(term) }\n\n", q, q,
		e.sx("NewText"), strings.Join(parts, ", "))
}

// literal renders the literal function of an In, Eq or EqIn field.
func (e *emitter) literal(f *builderField) string {
	switch f.lit {
	case "UUIDLit":
		return e.sx("UUIDLit")
	case "EnumLit":
		labels := make([]string, len(f.labels))
		for i, label := range f.labels {
			labels[i] = "string(" + e.enumLabel(f.f.col, label) + ")"
		}
		return e.sx("EnumLit") + "[" + e.typ(f.value) + "](" + strings.Join(labels, ", ") + ")"
	}
	return e.sx(f.lit) + "[" + e.typ(f.value) + "]"
}

// orList joins names as "a, b or c".
func orList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// searchAtLine returns the search of a derived file whose code holds line:
// the last search marker at or before it. With no marker before it, the
// line is a binding's, or, in a file without a binding, the first search's.
func searchAtLine(src []byte, line int, searches []*searchDecl, bindings int) *searchDecl {
	var found *searchDecl
	for i, text := range strings.Split(string(src), "\n") {
		if i >= line {
			break
		}
		for _, s := range searches {
			if strings.HasPrefix(text, searchMarker(s.name)) {
				found = s
			}
		}
	}
	if found == nil && bindings == 0 && len(searches) > 0 {
		return searches[0]
	}
	return found
}

// article is "an" before a name that starts with a vowel, else "a".
func article(name string) string {
	if strings.ContainsAny(name[:1], "AEIOU") {
		return "an"
	}
	return "a"
}
