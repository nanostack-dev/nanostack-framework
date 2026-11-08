package genvalidate

import (
	"fmt"
	"go/types"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// enumChecks renders enum and const for a value of kind "string",
// "boolean", "number" or "integer:<basic name>".
func (g *generator) enumChecks(sc *Schema, x, kind, p string) string {
	return g.enumCheck(sc, x, kind, p) + g.constCheck(sc, x, kind, p)
}

func (g *generator) enumCheck(sc *Schema, x, kind, p string) string {
	if sc.Enum == nil {
		return ""
	}
	var lits, shown []string
	for _, v := range sc.Enum {
		switch v.Kind {
		case "null":
			continue
		case "other":
			g.diag(sc.Loc, "enum with object or array values is not enforced")
			return ""
		}
		shown = append(shown, v.Str)
		if lit, ok := enumLiteral(kind, v); ok && !slices.Contains(lits, lit) {
			lits = append(lits, lit)
		}
	}
	msg := "must be one of: " + strings.Join(shown, ", ")
	switch {
	case len(lits) > 0:
		return fmt.Sprintf("switch %s {\ncase %s:\ndefault:\nerrs.Add(%s, validate.CodeEnum, %q)\n}\n",
			x, strings.Join(lits, ", "), p, msg)
	case len(shown) > 0:
		// No value can have the Go type, so every value is rejected.
		return fmt.Sprintf("errs.Add(%s, validate.CodeEnum, %q)\n", p, msg)
	}
	return ""
}

func (g *generator) constCheck(sc *Schema, x, kind, p string) string {
	c := sc.Const
	switch {
	case c == nil || c.Kind == "null":
		return ""
	case c.Kind == "other":
		g.diag(sc.Loc, "const with an object or array value is not enforced")
		return ""
	}
	msg := "must be " + c.Str
	if lit, ok := enumLiteral(kind, *c); ok {
		return fmt.Sprintf("if %s != %s {\nerrs.Add(%s, validate.CodeConst, %q)\n}\n", x, lit, p, msg)
	}
	return fmt.Sprintf("errs.Add(%s, validate.CodeConst, %q)\n", p, msg)
}

// enumLiteral renders v as a constant of a value of the given kind, or
// reports false when no value of that kind can equal v.
func enumLiteral(kind string, v Value) (string, bool) {
	switch {
	case kind == "string" && v.Kind == "string":
		return strconv.Quote(v.Str), true
	case kind == "boolean" && v.Kind == "boolean":
		return v.Str, true
	case kind == "number" && v.Kind == "number":
		r, ok := new(big.Rat).SetString(v.Str)
		if !ok {
			return "", false
		}
		f, _ := r.Float64()
		return floatLit(f), true
	case strings.HasPrefix(kind, "integer:") && v.Kind == "number":
		return integerLiteral(strings.TrimPrefix(kind, "integer:"), v.Str)
	}
	return "", false
}

func integerLiteral(basic, lit string) (string, bool) {
	r, ok := new(big.Rat).SetString(lit)
	if !ok || !r.IsInt() {
		return "", false
	}
	kind, known := basicKinds[basic]
	if !known || !fits(types.Typ[kind], r.Num()) {
		return "", false
	}
	return r.Num().String(), true
}

var basicKinds = map[string]types.BasicKind{
	"int": types.Int, "int8": types.Int8, "int16": types.Int16, "int32": types.Int32, "int64": types.Int64,
	"uint": types.Uint, "uint8": types.Uint8, "uint16": types.Uint16, "uint32": types.Uint32, "uint64": types.Uint64,
	"uintptr": types.Uintptr,
}

// untyped checks a value decoded into an interface: its JSON type, enum and
// const. Other constraints are reported as not enforced.
func (g *generator) untyped(sc *Schema, x, p string) string {
	x = bare(x)
	var b strings.Builder
	if types := nonNullTypes(sc.Types); len(types) > 0 {
		quoted := make([]string, len(types))
		for i, t := range types {
			quoted[i] = strconv.Quote(t)
		}
		fmt.Fprintf(&b, "if %s != nil && !validate.JSONType(%s, %s) {\nerrs.Add(%s, validate.CodeType, %q)\n}\n",
			x, x, strings.Join(quoted, ", "), p, "must be "+strings.Join(types, " or "))
	}
	b.WriteString(g.untypedEnum(sc, x, p))
	if lost := untypedLost(sc); len(lost) > 0 {
		g.diag(sc.Loc, strings.Join(lost, ", ")+" not enforced on an untyped Go value (interface{})")
	}
	return b.String()
}

func nonNullTypes(ts []string) []string {
	if len(ts) == 1 && ts[0] == "null" {
		return nil
	}
	return ts
}

// untypedLost names the constraints an interface value cannot be checked
// against.
func untypedLost(sc *Schema) []string {
	var lost []string
	for _, k := range stringConstraints(sc) {
		if k != "enum" && k != "const" {
			lost = append(lost, k)
		}
	}
	if sc.Minimum != nil || sc.Maximum != nil || sc.ExclusiveMinimum != nil || sc.ExclusiveMaximum != nil ||
		sc.MultipleOf != nil {
		lost = append(lost, "numeric bounds")
	}
	if sc.Items != nil || sc.MinItems != nil || sc.MaxItems != nil || sc.UniqueItems {
		lost = append(lost, "array constraints")
	}
	if len(sc.Properties) > 0 || len(sc.Required) > 0 || sc.AdditionalPropertiesFalse ||
		sc.AdditionalProperties != nil {
		lost = append(lost, "object constraints")
	}
	if len(sc.OneOf)+len(sc.AnyOf) > 0 {
		lost = append(lost, "oneOf/anyOf")
	}
	return lost
}

// untypedEnum checks enum and const on a value decoded into an interface,
// where JSON numbers are float64.
func (g *generator) untypedEnum(sc *Schema, x, p string) string {
	var b strings.Builder
	if sc.Enum != nil {
		lits := make([]string, 0, len(sc.Enum))
		shown := make([]string, 0, len(sc.Enum))
		for _, v := range sc.Enum {
			l, ok := untypedLiteral(v)
			if !ok {
				g.diag(sc.Loc, "enum with object or array values is not enforced")
				return ""
			}
			lits, shown = append(lits, l), append(shown, v.Str)
		}
		if len(lits) > 0 {
			fmt.Fprintf(&b, "switch %s {\ncase %s:\ndefault:\nerrs.Add(%s, validate.CodeEnum, %q)\n}\n",
				x, strings.Join(lits, ", "), p, "must be one of: "+strings.Join(shown, ", "))
		}
	}
	if sc.Const != nil {
		l, ok := untypedLiteral(*sc.Const)
		if !ok {
			g.diag(sc.Loc, "const with an object or array value is not enforced")
		} else {
			fmt.Fprintf(
				&b,
				"if %s != %s {\nerrs.Add(%s, validate.CodeConst, %q)\n}\n",
				x,
				l,
				p,
				"must be "+sc.Const.Str,
			)
		}
	}
	return b.String()
}

func untypedLiteral(v Value) (string, bool) {
	switch v.Kind {
	case "string":
		return strconv.Quote(v.Str), true
	case "boolean":
		return v.Str, true
	case "number":
		r, ok := new(big.Rat).SetString(v.Str)
		if !ok {
			return "", false
		}
		f, _ := r.Float64()
		return "float64(" + floatLit(f) + ")", true
	case "null":
		return "nil", true
	}
	return "", false
}

func stringConstraints(sc *Schema) []string {
	var out []string
	if sc.MinLength != nil && *sc.MinLength > 0 {
		out = append(out, "minLength")
	}
	if sc.MaxLength != nil {
		out = append(out, "maxLength")
	}
	if sc.Pattern != "" {
		out = append(out, "pattern")
	}
	if sc.Format != "" && formatChecks[sc.Format].fn != "" {
		out = append(out, "format")
	}
	if sc.Enum != nil {
		out = append(out, "enum")
	}
	if sc.Const != nil {
		out = append(out, "const")
	}
	return out
}

func hasAnyConstraint(sc *Schema) bool {
	c := *sc
	c.Types, c.Nullable = nil, false
	return hasConstraints(&c) || sc.Ref != ""
}
