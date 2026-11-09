package genrepo

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// builderSorts sets the sort enum, the keys, the tiebreak and the default
// sort.
func (c *collector) builderSorts(b *builder) {
	s := b.s
	// SortHandWritten adds a constant and no key.
	if !slices.ContainsFunc(s.sorts, func(x *searchSort) bool { return x.kind != "SortHandWritten" }) {
		c.errorf(s.tableNode, "add nanorepo.Sort(...) with the columns an API client may sort on, such as "+
			"nanorepo.Sort("+s.tableText+"."+s.order[0].field+")",
			"search %s declares no sort with a key (SortHandWritten adds none): searchexec.DefineSpec needs "+
				"at least one key", s.name)
	}
	for _, sort := range s.sorts {
		c.builderSort(b, sort)
	}
	for _, col := range s.order {
		ref := &colRef{table: s.root.table, tableText: s.tableText, col: col}
		b.tiebreak = append(b.tiebreak, c.builderKey(b, ref, "", tiebreakText(s)))
	}
	b.dflt = defaultConst(b)
	seen, apis := map[string]string{}, map[string]bool{}
	for _, sort := range b.sorts {
		if apis[sort.api] {
			c.errorf(s.tableNode, "remove one of the two sorts, or give one another API name with "+
				"SortKey(...).API(...)", "search %s: the sort_by value %q is given twice", s.name, sort.api)
			continue
		}
		apis[sort.api] = true
		if first, dup := seen[sort.name]; dup {
			c.errorf(s.tableNode, "give one of the two sorts another API name with SortKey(...).API(...)",
				"search %s: the sorts %q and %q both make the constant %sSort%s", s.name, first, sort.api, s.name,
				sort.name)
			continue
		}
		seen[sort.name] = sort.api
	}
}

// builderSort adds the constants of one sort, with its key.
func (c *collector) builderSort(b *builder, sort *searchSort) {
	switch sort.kind {
	case "SortExpr":
		if key := c.exprKey(b, sort); key != nil {
			b.sorts = append(b.sorts, sortConst{name: sort.field, api: sort.api[0], key: key})
		}
		return
	case "SortHandWritten":
		b.sorts = append(b.sorts, sortConst{name: goName(sort.api[0]), api: sort.api[0]})
		return
	}
	key := c.builderKey(b, sort.col, sort.nulls, sort.kind+"("+c.source(sort.col.expr)+")")
	for _, api := range sort.api {
		name := goName(api)
		if sort.kind == "Sort" {
			name = sort.col.col.field
		}
		b.sorts = append(b.sorts, sortConst{name: name, api: api, key: key})
	}
}

// defaultConst returns the constant of the DefaultSort column, nil without
// DefaultSort.
func defaultConst(b *builder) *sortConst {
	d := b.s.defaultSort
	if d == nil {
		return nil
	}
	for i, sort := range b.sorts {
		if d.col == nil {
			if sort.key != nil && sort.key.expr != nil && sort.key.field == d.field {
				return &b.sorts[i]
			}
			continue
		}
		k := sort.key
		if k != nil && k.col != nil && k.col.table == d.col.table && k.col.alias == d.col.alias &&
			k.col.col.field == d.col.col.field {
			return &b.sorts[i]
		}
	}
	return nil
}

// builderKey picks the keyset key of a column from its kind, its catalogue
// type and its Row field (design S7, S9.1). what names the option that
// sorts on the column, for a message. nil after a problem.
func (c *collector) builderKey(b *builder, col *colRef, nulls, what string) *sortKey {
	s := b.s
	where := ast.Node(s.tableNode)
	if col.expr != nil {
		where = col.expr
	}
	inst := s.instanceOf(col)
	if inst == nil {
		return nil // the read set check reports it
	}
	rf := b.row.cols[instCol{inst, col.col.sqlName}]
	if rf == nil {
		c.noRowField(b, where, inst, col.col, what)
		return nil
	}
	if rf.bad || rf.typ == nil {
		return nil // builderRow reports it
	}
	k := &sortKey{col: col, field: rf.path, nulls: nulls}
	nullable := inst.columnNullable(col.col)
	_, isPtr := types.Unalias(rf.typ).(*types.Pointer)
	switch {
	case nullable && !isPtr:
		c.errorf(where, "make the Row field a pointer, as nano gen db does in a model for a NULL column",
			"search %s: %s is nullable, and its Row field is %s, not a pointer", s.name,
			col.col.sqlName, c.qualified(rf.typ))
		return nil
	case !nullable && nulls != "":
		c.errorf(where, "remove ."+nulls+"(): a NOT NULL column has no NULL to place",
			"search %s: %s on %s, which is NOT NULL", s.name, nulls, col.col.sqlName)
		return nil
	case !nullable && holdsNull(rf.typ):
		c.errorf(where, "make the Row field "+c.fieldType(col.col, false)+": the column is NOT NULL",
			"search %s: the key on %s.%s reads the Row field %s of type %s, and the key needs %s", s.name,
			inst.name(), col.col.sqlName, rf.path, c.qualified(rf.typ), c.fieldType(col.col, false))
		return nil
	}
	if elem, _ := optional(rf.typ); !types.Identical(elem, col.col.value) {
		c.errorf(where, "make the Row field "+c.fieldType(col.col, nullable)+", the type of the model field",
			"search %s: the key on %s.%s reads the Row field %s of type %s, and the key needs %s", s.name,
			inst.name(), col.col.sqlName, rf.path, c.qualified(rf.typ), c.fieldType(col.col, nullable))
		return nil
	}
	if !c.pickKey(col, k, nullable) {
		c.errorf(where, "sort on another column, or write the method by hand on searchexec.Resolve",
			"search %s: nano gen repo has no keyset key for %s (%s, Go %s)", s.name, col.col.sqlName,
			col.table.catalog[col.col.sqlName], c.qualified(col.col.value))
		return nil
	}
	return k
}

// noRowField reports a key column that no Row field reads (E25).
func (c *collector) noRowField(b *builder, where ast.Node, inst *instance, col column, what string) {
	s := b.s
	field := c.suggestField(b, inst, col)
	fix := "add " + field + " to " + c.qualified(s.row)
	if b.row.model {
		fix = "search a Row of your own: type " + lowerFirst(s.name) + "Row struct { " + c.qualified(s.row) + "; " +
			field + " }"
	}
	c.errorf(where, fix, "search %s: %s needs a Row field for %s.%s; %s has none", s.name, what, inst.name(),
		col.sqlName, c.qualified(s.row))
}

// tiebreakText names the tiebreak of s for a message.
func tiebreakText(s *searchDecl) string {
	if s.orderDefault {
		return "the tiebreak " + s.orderKey.String()
	}
	return "Tiebreak"
}

// pickKey sets the constructor and the accessor of k from the column.
func (c *collector) pickKey(col *colRef, k *sortKey, nullable bool) bool {
	ctor, conv := c.keyCtor(col, k)
	if ctor == "" {
		return false
	}
	k.ctor, k.value = ctor, col.col.value
	if conv != "" {
		k.value = types.Typ[convKind(conv)]
	}
	if nullable {
		k.ctor, k.ptrConv, k.value = "Nullable"+ctor, conv, types.NewPointer(k.value)
		return true
	}
	k.conv = conv
	return true
}

// keyCtor returns the db/keyset constructor of a key on col, without its
// Nullable prefix, and the conversion its accessor needs, or "" when there
// is no key for the column.
func (c *collector) keyCtor(col *colRef, k *sortKey) (ctor, conv string) {
	v := col.col.value
	basic, _ := v.Underlying().(*types.Basic)
	switch col.col.kind {
	case "ColumnString":
		return c.stringKeyCtor(col, k, basic)
	case "ColumnTimestampz":
		if col.col.lit == litTimestampz {
			return "TimeKey", ""
		}
	case "ColumnTimestamp":
		if col.col.lit == litTimestamp {
			return "LocalTimeKey", ""
		}
	case "ColumnInteger":
		if basic != nil && basic.Info()&types.IsInteger != 0 && basic.Info()&types.IsUnsigned == 0 {
			return "IntKey", convTo(v, types.Int64)
		}
	case "ColumnFloat":
		if basic != nil && basic.Kind() == types.Float64 {
			return "FloatKey", convTo(v, types.Float64)
		}
		if basic != nil && basic.Kind() == types.Float32 {
			return "RealKey", convTo(v, types.Float32)
		}
	}
	return "", ""
}

// stringKeyCtor is keyCtor for a ColumnString column.
func (c *collector) stringKeyCtor(col *colRef, k *sortKey, basic *types.Basic) (ctor, conv string) {
	v, dbType := col.col.value, col.table.catalog[col.col.sqlName]
	switch {
	case col.col.lit == litUUID:
		return "UUIDKey", ""
	case dbType == "inet" && isBasic(v, types.String):
		return "InetKey", ""
	case col.col.lit == litEnum:
		if c.enumKey(col, k) {
			return "EnumKey", "string"
		}
	case textType(dbType) && basic != nil && basic.Kind() == types.String:
		return "StringKey", convTo(v, types.String)
	}
	return "", ""
}

// convTo is the conversion of a value of type v to the basic type kind, or
// "" when v is that type.
func convTo(v types.Type, kind types.BasicKind) string {
	if isBasic(v, kind) {
		return ""
	}
	return types.Typ[kind].Name()
}

func convKind(name string) types.BasicKind {
	switch name {
	case "int64":
		return types.Int64
	case "float64":
		return types.Float64
	case "float32":
		return types.Float32
	}
	return types.String
}

// enumKey sets the enum's type name and labels: the constants of the model
// type in the order of its <Type>AllValues, which nano gen db writes in the
// enum's declaration order.
func (c *collector) enumKey(col *colRef, k *sortKey) bool {
	dbType := col.table.catalog[col.col.sqlName]
	if !strings.Contains(dbType, ".") {
		dbType = col.table.schema + "." + dbType
	}
	if !enumTypeName.MatchString(dbType) {
		return false
	}
	labels := c.enumLabels(col)
	k.enumType, k.labels = dbType, labels
	return len(labels) > 0
}

// enumLabels returns the constants of the model type of an enum column, as
// pkg.Name, in the order of its <Type>AllValues, or nil.
func (c *collector) enumLabels(col *colRef) []string {
	named, ok := types.Unalias(col.col.value).(*types.Named)
	if !ok {
		return nil
	}
	files, err := c.l.parsed(named.Obj().Pkg().Path())
	if err != nil {
		return nil
	}
	all := named.Obj().Name() + "AllValues"
	for _, f := range files {
		for _, decl := range f.Decls {
			if labels, found := allValues(f.Name.Name, decl, all); found {
				return labels
			}
		}
	}
	return nil
}

// allValues reads var <name> = []T{A, B} from decl and returns pkg.A,
// pkg.B.
func allValues(pkg string, decl ast.Decl, name string) ([]string, bool) {
	gd, ok := decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return nil, false
	}
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		if !isValue || len(vs.Names) != 1 || vs.Names[0].Name != name || len(vs.Values) != 1 {
			continue
		}
		lit, isLit := vs.Values[0].(*ast.CompositeLit)
		if !isLit {
			return nil, true
		}
		var out []string
		for _, elt := range lit.Elts {
			id := identOf(elt)
			if id == nil {
				return nil, true
			}
			out = append(out, pkg+"."+id.Name)
		}
		return out, true
	}
	return nil, false
}

// modelField returns the type of the field of the struct row, or nil.
func modelField(row types.Type, name string) types.Type {
	st, ok := types.Unalias(row).Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for f := range st.Fields() {
		if f.Name() == name {
			return f.Type()
		}
	}
	return nil
}
