package genrepo

import (
	"go/types"
	"slices"
	"strings"
)

// builderFilters sets the accessor of each filter: its field type from its
// operators, its value type, and its literal.
func (c *collector) builderFilters(b *builder) {
	s := b.s
	names := map[string]bool{}
	for _, f := range s.filters {
		if slices.Contains(reservedFields, f.name) {
			c.errorf(f.node, "filter through nanorepo.FilterExpr with another name, such as \""+f.name+"Value\"",
				"search %s: the filter %s would hide the method %s of the builder", s.name, f.name, f.name)
			continue
		}
		if names[f.name] {
			c.errorf(f.node, "merge the two filters, or name one with nanorepo.FilterExpr",
				"search %s: two filters make the accessor %s", s.name, f.name)
			continue
		}
		names[f.name] = true
		if bf := c.builderField(s, f); bf != nil {
			b.filters = append(b.filters, bf)
		}
	}
}

// builderField works out the accessor of one filter, nil after a problem.
func (c *collector) builderField(s *searchDecl, f *searchFilter) *builderField {
	bf := &builderField{f: f}
	switch f.kind {
	case "FilterExists":
		switch {
		case len(f.ops) == 1 && f.ops[0].name == "In":
			bf.kind = "Exists"
		case len(f.ops) == 1 && f.ops[0].name == "InFold":
			bf.kind, bf.value = "ExistsFold", f.col.col.value
			if f.as != nil {
				bf.value = f.as
			}
			return bf
		default:
			c.errorf(f.node, "pass one operator: nanorepo.In, or nanorepo.InFold on a text column",
				"search %s: FilterExists keeps rows that have a child row whose value is in a list: it takes "+
					"In or InFold alone", s.name)
			return nil
		}
		return c.filterValue(s, f, bf)
	case "Flag":
		bf.kind, bf.fn = "Flag", c.funcObj(s, "Flag", f.expr)
		return bf
	case "FilterExpr":
		if bf.fn = c.funcObj(s, "FilterExpr", f.expr); bf.fn == nil {
			return nil
		}
	}
	ops := map[string]bool{}
	for _, op := range f.ops {
		ops[op.name] = true
	}
	switch {
	case len(ops) == 1 && ops["Eq"]:
		bf.kind = "Eq"
	case len(ops) == 1 && ops["In"]:
		bf.kind = "In"
	case len(ops) == 2 && ops["Eq"] && ops["In"]:
		bf.kind = "EqIn"
	case len(ops) == 1 && ops["InFold"]:
		bf.kind = "InFold"
	case len(ops) >= 1 && len(ops) <= 2 && !hasOtherThan(ops, "Contains", "ContainsAny"):
		bf.kind = "Text"
	case len(ops) == 1 && ops["Present"]:
		bf.kind = "Presence"
	case len(ops) == 1 && ops["Range"]:
		bf.kind = "TimeRange"
	default:
		c.errorf(f.node, "split the operators: Eq and In share a field, and Contains and ContainsAny do; "+
			"give the others a nanorepo.FilterExpr of their own",
			"search %s: the operators of %s make more than one field type", s.name, f.name)
		return nil
	}
	return c.filterValue(s, f, bf)
}

func hasOtherThan(ops map[string]bool, allowed ...string) bool {
	for op := range ops {
		if !slices.Contains(allowed, op) {
			return true
		}
	}
	return false
}

// filterValue sets the value type and the literal of a filter, and checks
// that its expression has the type its field takes.
func (c *collector) filterValue(s *searchDecl, f *searchFilter, bf *builderField) *builderField {
	exprType := ""
	var v types.Type
	if f.col != nil {
		v = f.col.col.value
		if f.as != nil {
			v = f.as
		}
		exprType = exprOfKind(f.col.col.kind)
	} else if fn, ok := bf.fn.(*types.Func); ok && fn.Signature().Results().Len() == 1 {
		exprType, v = exprTypeOf(fn.Signature().Results().At(0).Type())
	}
	need := map[string]string{"InFold": "StringExpression", "Text": "StringExpression",
		"TimeRange": "TimestampzExpression"}[bf.kind]
	if need != "" && exprType != need {
		op := map[string]string{"InFold": "InFold", "Text": "Contains", "TimeRange": "Range"}[bf.kind]
		c.errorf(f.node, "use another operator, or a FilterExpr whose function returns postgres."+need,
			"search %s: %s on %s needs a postgres.%s, and %s is %s", s.name, op, f.name, need, c.filterTarget(f),
			orUnknown(exprType))
		return nil
	}
	switch bf.kind {
	case "InFold":
		// The read set check (E2) and FilterAs leave only string types here:
		// a text column's model field, or the string of a StringExpression.
		bf.value = v
	case "Eq", "In", "EqIn", "Exists":
		bf.value, bf.lit = v, c.literalFunc(f, bf, v)
		if bf.lit == "" {
			c.errorf(f.node, "filter through a FilterExpr whose function returns a string, integer or "+
				"bool expression, such as postgres.CAST(...).AS_TEXT()",
				"search %s: nanostack gen repo has no literal for %s values of %s", s.name, c.qualified(typeOrNil(v)),
				c.filterTarget(f))
			return nil
		}
	}
	return bf
}

func orUnknown(s string) string {
	if s == "" {
		return "another type"
	}
	return "a postgres." + s
}

// filterTarget renders what a filter reads, for a message.
func (c *collector) filterTarget(f *searchFilter) string {
	if f.col != nil {
		return f.col.col.sqlName
	}
	return c.source(f.expr) + "()"
}

// literalFunc returns the searchexec literal function of a value of type v
// for the filter, or "". For a native enum column it sets the labels of bf.
func (c *collector) literalFunc(f *searchFilter, bf *builderField, v types.Type) string {
	basic, _ := typeOrNil(v).Underlying().(*types.Basic)
	switch {
	case f.col != nil && f.col.col.kind == "ColumnString" && f.col.col.lit == litUUID:
		if named, ok := types.Unalias(v).(*types.Named); ok && isNamed(named, uuidPath, "UUID") {
			return "UUIDLit"
		}
	case f.col != nil && f.col.col.kind == "ColumnString" && f.col.col.lit == litEnum:
		if bf.labels = c.enumLabels(f.col); len(bf.labels) > 0 && isStringLike(v) {
			return "EnumLit"
		}
	case f.col != nil && f.col.col.kind == "ColumnString" && !textType(f.col.table.catalog[f.col.col.sqlName]):
	case basic == nil:
	case basic.Info()&types.IsString != 0:
		return "StringLit"
	case basic.Kind() == types.Bool:
		return "BoolLit"
	case basic.Kind() == types.Int || basic.Kind() == types.Int32 || basic.Kind() == types.Int64:
		return "IntLit"
	}
	return ""
}

func isStringLike(t types.Type) bool {
	basic, ok := typeOrNil(t).Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

// exprOfKind is the postgres expression type of a column kind.
func exprOfKind(kind string) string {
	return strings.TrimPrefix(kind, "Column") + "Expression"
}

// exprTypeOf maps the result of an expression function to its postgres
// expression type and the Go type of its values.
func exprTypeOf(t types.Type) (string, types.Type) {
	name := dbTypeName(t)
	switch name {
	case "StringExpression":
		return name, types.Typ[types.String]
	case "IntegerExpression":
		return name, types.Typ[types.Int64]
	case "BoolExpression":
		return name, types.Typ[types.Bool]
	case "TimestampzExpression", "FloatExpression":
		return name, nil
	}
	return "", nil
}

// dbTypeName returns the name of t when it is a type of the Nanostack DB
// module, such as StringExpression for postgres.StringExpression (an alias
// of the jet type of the same name), or "".
func dbTypeName(t types.Type) string {
	if a, ok := t.(*types.Alias); ok && inDB(a.Obj().Pkg()) {
		return a.Obj().Name()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && inDB(n.Obj().Pkg()) {
		return n.Obj().Name()
	}
	return ""
}
