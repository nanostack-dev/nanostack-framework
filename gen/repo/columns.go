package genrepo

import (
	"go/ast"
	"go/types"
	"reflect"
	"strings"
)

// columns checks the row type of a declaration against the table and sets
// each column's model type and literal.
func (c *collector) columns(call *ast.CallExpr, t *tableBase) bool {
	st, ok := t.row.Underlying().(*types.Struct)
	if !ok {
		c.errorf(call, "map from the generated model of table "+t.sqlName,
			"the row type %s is not a struct", c.qualified(t.row))
		return false
	}
	type modelField struct {
		typ     types.Type
		primary bool
	}
	fields := map[string]modelField{}
	for i := range st.NumFields() {
		f := st.Field(i)
		fields[f.Name()] = modelField{typ: f.Type(), primary: reflect.StructTag(st.Tag(i)).Get("sql") == "primary_key"}
	}
	var rowPkg *types.Package
	if n, isNamed := types.Unalias(t.row).(*types.Named); isNamed {
		rowPkg = n.Obj().Pkg()
	}
	for i := range t.columns {
		col := &t.columns[i]
		f, found := fields[col.field]
		if !found {
			c.errorf(call, "map from the generated model of table "+t.sqlName,
				"the row type %s has no field %s for column %s of table %s",
				c.qualified(t.row), col.field, col.sqlName, t.sqlName)
			return false
		}
		col.value, col.nullable = optional(f.typ)
		col.primary = f.primary
		col.lit = literalOf(col.kind, col.value, rowPkg)
		if col.lit == litEnum {
			col.enum, found = c.l.enumType(col.value, t.schema)
			if !found {
				col.lit = litNone
			}
		}
	}
	if !c.catalogTypes(call, t) {
		return false
	}
	seen := map[string]string{}
	for _, col := range t.columns {
		for _, p := range predicateNames(col) {
			if other, dup := seen[p]; dup {
				c.errorf(call, "rename one of the two columns in a migration, or write the predicate by hand on b.t",
					"columns %s and %s of table %s both give the predicate %s", other, col.sqlName, t.sqlName, p)
				return false
			}
			seen[p] = col.sqlName
		}
	}
	return true
}

// catalogTypes sets the catalogue type of each column from the ColumnTypes
// method of the table type. Whether a string column gets Contains and
// EqualsIgnoreCase depends on its type, so such a column without a type is
// an error. A view is the exception: nano gen db does not write ColumnTypes
// on a view yet, so no column of a view without it gets the two predicates.
// A string column whose type is not text, such as inet or jsonb, gets no
// equality predicate either: a text value does not compare with it.
func (c *collector) catalogTypes(call *ast.CallExpr, t *tableBase) bool {
	if t.view && !t.catalogOK {
		return true
	}
	var missing []string
	for i := range t.columns {
		col := &t.columns[i]
		col.dbType = t.catalog[col.sqlName]
		switch {
		case col.candidate() && col.dbType == "":
			missing = append(missing, col.sqlName)
		case col.lit == litString && col.dbType != "" && !textType(col.dbType):
			col.lit, col.notText = litNone, true
		}
	}
	switch {
	case len(missing) == 0:
		return true
	case !t.catalogOK:
		c.errorf(call, "run nano gen db, which writes ColumnTypes into the table package, then rerun nano gen repo",
			"the table package has no ColumnTypes for %s, so nano gen repo cannot tell if %s is text or "+
				"another type that the model reads as a string, such as inet or jsonb",
			t.sqlName, strings.Join(missing, ", "))
	default:
		c.errorf(call, "run nano gen db, which writes ColumnTypes again from the migrations, then rerun nano gen repo",
			"ColumnTypes of %s gives no type for %s: the table package is older than the table",
			t.sqlName, strings.Join(missing, ", "))
	}
	return false
}

// optional strips a pointer or a functional.Option from a model field type.
func optional(t types.Type) (types.Type, bool) {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem(), true
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil &&
		n.Obj().Pkg().Path() == functionalPath && n.Obj().Name() == "Option" && n.TypeArgs().Len() == 1 {
		return n.TypeArgs().At(0), true
	}
	return t, false
}

// literalOf picks the constructor a predicate on a column of kind, with
// model values of type v, passes its value through. An enum is a named
// string type of the model package. Other types, such as arrays, JSON and
// decimals, get no equality predicate.
func literalOf(kind string, v types.Type, rowPkg *types.Package) literal {
	if b, ok := types.Unalias(v).(*types.Basic); ok {
		return basicLiteral(kind, b.Kind())
	}
	n, ok := types.Unalias(v).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return litNone
	}
	path, name := n.Obj().Pkg().Path(), n.Obj().Name()
	switch {
	case path == "time" && name == "Time":
		return timeLiteral(kind)
	case path == uuidPath && name == "UUID" && kind == "ColumnString":
		return litUUID
	case kind == "ColumnString" && n.Obj().Pkg() == rowPkg:
		if b, isBasic := n.Underlying().(*types.Basic); isBasic && b.Kind() == types.String {
			return litEnum
		}
	}
	return litNone
}

// basicLiteral is literalOf for a basic Go type.
func basicLiteral(kind string, b types.BasicKind) literal {
	switch {
	case kind == "ColumnString" && b == types.String:
		return litString
	case kind == "ColumnBool" && b == types.Bool:
		return litBool
	case kind == "ColumnInteger" && b == types.Int8:
		return litInt8
	case kind == "ColumnInteger" && b == types.Int16:
		return litInt16
	case kind == "ColumnInteger" && b == types.Int32:
		return litInt32
	case kind == "ColumnInteger" && b == types.Int64:
		return litInt64
	case kind == "ColumnFloat" && b == types.Float32:
		return litReal
	case kind == "ColumnFloat" && b == types.Float64:
		return litDouble
	}
	return litNone
}

// timeLiteral is literalOf for a time.Time model field.
func timeLiteral(kind string) literal {
	switch kind {
	case "ColumnTimestampz":
		return litTimestampz
	case "ColumnTimestamp":
		return litTimestamp
	case "ColumnDate":
		return litDate
	case "ColumnTime":
		return litTime
	case "ColumnTimez":
		return litTimez
	default:
		return litNone
	}
}

// predicateNames are the methods of the where type for c.
func predicateNames(c column) []string {
	var out []string
	if c.lit != litNone {
		out = append(out, c.field, c.field+"In")
	}
	if c.textual() {
		out = append(out, c.field+"Contains", c.field+"EqualsIgnoreCase")
	}
	if c.lit.ordered() {
		out = append(out, c.field+"After", c.field+"Before")
	}
	if c.nullable {
		out = append(out, c.field+"IsNull")
	}
	return out
}
