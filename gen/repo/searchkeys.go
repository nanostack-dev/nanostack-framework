package genrepo

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// builderSorts sets the sort enum, the keys, the tiebreak and the default
// sort.
func (c *collector) builderSorts(b *builder) {
	s := b.s
	if len(s.sorts) == 0 {
		c.errorf(s.tableNode, "add nanorepo.Sort(...) with the columns an API client may sort on, such as "+
			"nanorepo.Sort("+s.tableText+"."+s.order[0].field+")",
			"search %s declares no sort: searchexec.DefineSpec needs at least one key", s.name)
	}
	for _, sort := range s.sorts {
		c.builderSort(b, sort)
	}
	for _, col := range s.order {
		ref := &colRef{table: s.root.table, tableText: s.tableText, col: col}
		b.tiebreak = append(b.tiebreak, c.builderKey(b, ref, ""))
	}
	b.dflt = defaultConst(b)
	seen := map[string]string{}
	for _, sort := range b.sorts {
		if first, dup := seen[sort.name]; dup && first != sort.api {
			c.errorf(s.tableNode, "give one of the two sorts another API name with SortKey(...).API(...)",
				"search %s: the sorts %q and %q both make the constant %sSort%s", s.name, first, sort.api, s.name,
				sort.name)
		}
		seen[sort.name] = sort.api
	}
}

// builderSort adds the constants of one sort, with its key.
func (c *collector) builderSort(b *builder, sort *searchSort) {
	switch sort.kind {
	case "SortExpr":
		c.errorf(sort.expr, "sort on a column, or write the method by hand on searchexec.Resolve with "+
			"keyset.IntExprKey (or its kin) in plan.Order",
			"search %s: SortExpr(%q) is read, and nano gen repo does not generate a computed key yet",
			b.s.name, sort.field)
		return
	case "SortHandWritten":
		b.sorts = append(b.sorts, sortConst{name: goName(sort.api[0]), api: sort.api[0]})
		return
	}
	key := c.builderKey(b, sort.col, sort.nulls)
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
	if d == nil || d.col == nil {
		return nil
	}
	for i, sort := range b.sorts {
		if sort.key != nil && sort.key.col.table == d.col.table && sort.key.col.alias == d.col.alias &&
			sort.key.col.col.field == d.col.col.field {
			return &b.sorts[i]
		}
	}
	return nil
}

// builderKey picks the keyset key of a column from its kind, its catalogue
// type and its model field (design S7). nil after a problem.
func (c *collector) builderKey(b *builder, col *colRef, nulls string) *sortKey {
	s := b.s
	where := ast.Node(s.tableNode)
	if col.expr != nil {
		where = col.expr
	}
	if !s.root.is(col.table, col.alias) {
		c.errorf(where, "sort on a column of "+s.root.table.sqlName+", or filter on "+c.qualifiedColumn(s, col),
			"search %s: a sort on %s needs a Row field for it, and the Row %s has none: nano gen repo "+
				"selects only the columns of %s yet", s.name, c.qualifiedColumn(s, col), c.qualified(s.row),
			s.root.table.sqlName)
		return nil
	}
	field, ok := modelField(s.row, col.col.field)
	if !ok {
		return nil // builderRow reports it
	}
	k := &sortKey{col: col, field: col.col.field, nulls: nulls}
	nullable := col.col.nullable
	if nullable {
		if _, isPtr := types.Unalias(field).(*types.Pointer); !isPtr {
			c.errorf(where, "regenerate the model with pointers for NULL columns",
				"search %s: %s is nullable, and its model field is %s, not a pointer", s.name,
				col.col.sqlName, c.qualified(field))
			return nil
		}
	} else if nulls != "" {
		c.errorf(where, "remove ."+nulls+"(): a NOT NULL column has no NULL to place",
			"search %s: %s on %s, which is NOT NULL", s.name, nulls, col.col.sqlName)
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
	named, ok := types.Unalias(col.col.value).(*types.Named)
	if !ok || !enumTypeName.MatchString(dbType) {
		return false
	}
	files, err := c.l.parsed(named.Obj().Pkg().Path())
	if err != nil {
		return false
	}
	all := named.Obj().Name() + "AllValues"
	for _, f := range files {
		for _, decl := range f.Decls {
			if labels, found := allValues(f.Name.Name, decl, all); found {
				k.enumType, k.labels = dbType, labels
				return len(labels) > 0
			}
		}
	}
	return false
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

// modelField returns the type of the field of the struct row, or false.
func modelField(row types.Type, name string) (types.Type, bool) {
	st, ok := types.Unalias(row).Underlying().(*types.Struct)
	if !ok {
		return nil, false
	}
	for f := range st.Fields() {
		if f.Name() == name {
			return f.Type(), true
		}
	}
	return nil, false
}
