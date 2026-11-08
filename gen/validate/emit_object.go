package genvalidate

import (
	"fmt"
	"go/types"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/oas"
)

// isNullable reports whether sc accepts a JSON null.
func (g *generator) isNullable(sc *oas.Schema) bool {
	return g.nullable(sc, 0)
}

func (g *generator) nullable(sc *oas.Schema, depth int) bool {
	switch {
	case sc == nil || depth > 32 || sc.Nullable:
		return true
	case sc.Ref != "":
		if sib := sc.Siblings(); sib != nil && sib.Nullable {
			return true
		}
		target := g.spec.Resolve(&oas.Schema{Ref: sc.Ref})
		return target != nil && g.nullable(target, depth+1)
	case len(sc.OneOf)+len(sc.AnyOf) > 0:
		return slices.ContainsFunc(append(slices.Clone(sc.OneOf), sc.AnyOf...), func(v *oas.Schema) bool {
			return g.nullable(v, depth+1)
		})
	case len(sc.AllOf) > 0:
		all := !slices.ContainsFunc(sc.AllOf, func(part *oas.Schema) bool { return !g.nullable(part, depth+1) })
		return all && len(sc.Types) == 0
	}
	return len(sc.Types) == 0 && sc.Enum == nil && sc.Const == nil
}

func (g *generator) resolve(sc *oas.Schema) *oas.Schema {
	if sc == nil {
		return &oas.Schema{}
	}
	if r := g.spec.Resolve(sc); r != nil {
		return r
	}
	return sc
}

// object checks a struct: properties, required members, unknown members,
// additional properties and union branches.
func (g *generator) object(sc *oas.Schema, x string, t types.Type, st *types.Struct, p string, c ctx) string {
	var b strings.Builder
	if isUnion(st) {
		b.WriteString(g.union(sc, x, t, p, c))
	} else if len(sc.OneOf)+len(sc.AnyOf) > 0 {
		g.diag(sc.Loc, "oneOf/anyOf is not enforced: Go type "+types.TypeString(t, nil)+" is not an oapi-codegen union")
	}
	required := map[string]bool{}
	for _, r := range sc.Required {
		required[r] = true
	}
	for _, prop := range sc.Properties {
		b.WriteString(g.property(prop, required[prop.Name], x, t, st, p, c))
	}
	b.WriteString(g.requiredOnly(sc, x, t, st, p, c))
	b.WriteString(g.memberCount(sc, p, g.additional(sc, x, t, st, p, c)))
	if len(sc.Types) > 0 && !slices.Contains(sc.Types, "object") {
		g.diag(sc.Loc, "schema type "+strings.Join(sc.Types, "|")+" does not match Go struct "+types.TypeString(t, nil))
	}
	return b.String()
}

// requiredOnly checks members that are required but not defined in
// properties: through a struct field, the AdditionalProperties map, or only
// the document.
func (g *generator) requiredOnly(sc *oas.Schema, x string, t types.Type, st *types.Struct, p string, c ctx) string {
	var b strings.Builder
	_, hasAP := fieldNamed(st, "AdditionalProperties")
	for _, name := range sc.Required {
		if slices.ContainsFunc(sc.Properties, func(pr oas.Property) bool { return pr.Name == name }) {
			continue
		}
		switch {
		case hasField(st, name):
			b.WriteString(g.property(oas.Property{Name: name, Schema: &oas.Schema{Loc: sc.Loc}}, true, x, t, st, p, c))
		case hasAP:
			fmt.Fprintf(&b, "{\n_, ok := %s.AdditionalProperties[%q]\nerrs.Required(%s.Field(%q), ok)\n}\n",
				x, name, p, name)
		default:
			fmt.Fprintf(&b, "errs.Required(%s.Field(%q), true)\n", p, name)
		}
	}
	return b.String()
}

// additional checks additionalProperties: false rejects unknown members, a
// schema checks every value of the AdditionalProperties map.
func (g *generator) additional(sc *oas.Schema, x string, t types.Type, st *types.Struct, p string, c ctx) string {
	var b strings.Builder
	if sc.AdditionalPropertiesFalse {
		names := make([]string, 0, len(sc.Properties))
		for _, prop := range sc.Properties {
			names = append(names, prop.Name)
		}
		// OnlyMembers binary-searches raw names: sort before quoting.
		sort.Strings(names)
		for i, n := range names {
			names[i] = strconv.Quote(n)
		}
		fmt.Fprintf(&b, "errs.OnlyMembers(%s%s)\n", p, prefixComma(names))
	}
	if sc.AdditionalProperties == nil {
		return b.String()
	}
	ap, hasAP := fieldNamed(st, "AdditionalProperties")
	if !hasAP {
		if hasAnyConstraint(g.resolve(sc.AdditionalProperties)) {
			g.diag(sc.Loc, "additionalProperties schema is not enforced: Go type "+
				types.TypeString(t, nil)+" has no AdditionalProperties field")
		}
		return b.String()
	}
	if mt, ok := types.Unalias(ap.Type()).Underlying().(*types.Map); ok {
		b.WriteString(g.mapValues(sc.AdditionalProperties, x+".AdditionalProperties", mt, p, c))
	}
	return b.String()
}

func prefixComma(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return ", " + strings.Join(items, ", ")
}

func hasField(st *types.Struct, name string) bool {
	_, ok := fieldByJSON(st, name)
	return ok
}

// memberCount checks minProperties and maxProperties. guarded is the code
// that walks the members (additionalProperties); it runs only when the
// object is within maxProperties, so a wide object costs no work in
// proportion to its width.
func (g *generator) memberCount(sc *oas.Schema, p, guarded string) string {
	if sc.MinProperties == nil && sc.MaxProperties == nil {
		return guarded
	}
	n := g.fresh("n")
	var minCheck string
	if v := sc.MinProperties; v != nil {
		minCheck = fmt.Sprintf(
			"if ok && %s < %d {\nerrs.Add(%s, validate.CodeMinProperties, %q)\n}\n",
			n,
			*v,
			p,
			"must have at least "+plural(*v, "property", "properties"),
		)
	}
	v := sc.MaxProperties
	if v == nil {
		return fmt.Sprintf("%s{\n%s, ok := %s.MemberCount()\n%s}\n", guarded, n, p, minCheck)
	}
	return fmt.Sprintf(
		"if %s, ok := %s.MemberCount(); ok && %s > %d {\nerrs.Add(%s, validate.CodeMaxProperties, %q)\n} else {\n%s%s}\n",
		n,
		p,
		n,
		*v,
		p,
		"must have at most "+plural(*v, "property", "properties"),
		guarded,
		minCheck,
	)
}

// property checks one member of a struct: presence, null, then the value.
func (g *generator) property(
	prop oas.Property,
	required bool,
	x string,
	owner types.Type,
	st *types.Struct,
	p string,
	c ctx,
) string {
	sc := prop.Schema
	f, ok := g.propertyField(prop, owner, st, c)
	if !ok {
		return ""
	}
	if g.resolve(sc).ReadOnly {
		required = false // readOnly members are not sent in requests
	}
	nullable := g.isNullable(sc)
	pv := g.fresh("f")
	a := g.fieldAccess(x+"."+f.Name(), types.Unalias(f.Type()), pv, required, nullable)
	body := g.value(sc, a.inner, a.innerT, pv, c.deeper())
	if a.prelude != "" && !uses(body, a.inner) {
		a.prelude = ""
	}

	head := fmt.Sprintf("%s := %s.Field(%q)", pv, p, prop.Name)
	presence := fmt.Sprintf("%s.Present(%s)", pv, a.fallback)
	if required {
		presence = fmt.Sprintf("errs.Required(%s, %s)", pv, a.fallback)
	}
	if body == "" {
		return presenceOnly(required, nullable, head, presence, pv, p, prop.Name, a.fallback)
	}
	conds := []string{presence}
	if !nullable {
		conds = append(conds, fmt.Sprintf("errs.NotNull(%s)", pv))
	}
	conds = append(conds, a.guards...)
	return fmt.Sprintf("if %s; %s {\n%s%s}\n", head, strings.Join(conds, " && "), a.prelude, body)
}

// propertyField finds the struct field of a property. A missing field is an
// error in a generated type and a diagnostic in a type of another package.
func (g *generator) propertyField(prop oas.Property, owner types.Type, st *types.Struct, c ctx) (*types.Var, bool) {
	sc := prop.Schema
	f, ok := fieldByJSON(st, prop.Name)
	switch {
	case !ok && c.external:
		g.diag(sc.Loc, fmt.Sprintf("property %q is not checked: Go type %s has no field for it",
			prop.Name, types.TypeString(owner, nil)))
		return nil, false
	case !ok:
		g.errorf("%s: property %q has no field in Go type %s", sc.Loc, prop.Name, types.TypeString(owner, nil))
		return nil, false
	case !f.Exported() && f.Pkg() != g.gp.pkg:
		g.diag(sc.Loc, fmt.Sprintf("property %q is not checked: its Go field is unexported", prop.Name))
		return nil, false
	}
	return f, true
}

// access is how generated code reads one struct field.
type access struct {
	fallback string     // presence without a document
	prelude  string     // binds the value, for nullable.Nullable
	inner    string     // the value expression once present
	innerT   types.Type // its type
	guards   []string   // further conditions before the value is read
}

func (g *generator) fieldAccess(fx string, ft types.Type, pv string, required, nullable bool) access {
	if ptr, ok := ft.(*types.Pointer); ok {
		a := access{
			fallback: fx + " != nil",
			inner:    "(*" + fx + ")",
			innerT:   ptr.Elem(),
			guards:   []string{fx + " != nil"},
		}
		if required && nullable {
			a.fallback = "true" // a nil pointer may be a null, which is present
		}
		return a
	}
	if arg, ok := nullableArg(ft); ok {
		v := g.fresh("v")
		return access{
			fallback: fx + ".IsSpecified()", inner: v, innerT: arg,
			guards: []string{"!" + fx + ".IsNull()"}, prelude: v + " := " + fx + ".MustGet()\n",
		}
	}
	a := access{fallback: valueFallback(fx, ft, required), inner: fx, innerT: ft}
	if nullable {
		a.guards = []string{"!" + pv + ".IsNull()"}
	}
	return a
}

// presenceOnly checks a property whose value has no constraints.
func presenceOnly(required, nullable bool, head, presence, pv, p, name, fallback string) string {
	switch {
	case required && !nullable:
		return fmt.Sprintf("if %s; %s {\nerrs.NotNull(%s)\n}\n", head, presence, pv)
	case required:
		return fmt.Sprintf("errs.Required(%s.Field(%q), %s)\n", p, name, fallback)
	case !nullable:
		return fmt.Sprintf("errs.NotNull(%s.Field(%q))\n", p, name)
	}
	return ""
}

// valueFallback is how a value-typed field tells presence without a
// document: nil for slices and maps, and for an optional scalar its zero
// value, which a Go caller uses for "not set".
func valueFallback(x string, t types.Type, required bool) string {
	switch u := t.Underlying().(type) {
	case *types.Slice, *types.Map, *types.Interface:
		return x + " != nil"
	case *types.Basic:
		if required {
			return "true"
		}
		switch {
		case u.Info()&types.IsString != 0:
			return x + ` != ""`
		case u.Info()&types.IsNumeric != 0:
			return x + " != 0"
		case u.Info()&types.IsBoolean != 0:
			return x
		}
	}
	return "true"
}

func (g *generator) mapObject(sc *oas.Schema, x string, mt *types.Map, p string, c ctx) string {
	var b strings.Builder
	required := map[string]bool{}
	for _, r := range sc.Required {
		required[r] = true
	}
	for _, prop := range sc.Properties {
		v := g.fresh("v")
		pv := g.fresh("f")
		inner := g.value(prop.Schema, v, mt.Elem(), pv, c.deeper())
		if required[prop.Name] {
			fmt.Fprintf(&b, "{\n_, ok := %s[%q]\nerrs.Required(%s.Field(%q), ok)\n}\n", x, prop.Name, p, prop.Name)
		}
		if inner != "" {
			fmt.Fprintf(
				&b,
				"if %s, ok := %s[%q]; ok {\n%s := %s.Field(%q)\n%s}\n",
				v,
				x,
				prop.Name,
				pv,
				p,
				prop.Name,
				inner,
			)
		}
	}
	for _, r := range sc.Required {
		if !slices.ContainsFunc(sc.Properties, func(pr oas.Property) bool { return pr.Name == r }) {
			fmt.Fprintf(&b, "{\n_, ok := %s[%q]\nerrs.Required(%s.Field(%q), ok)\n}\n", x, r, p, r)
		}
	}
	var walk strings.Builder
	if sc.AdditionalPropertiesFalse {
		names := make([]string, 0, len(sc.Properties))
		for _, prop := range sc.Properties {
			names = append(names, prop.Name)
		}
		// OnlyMembers binary-searches raw names: sort before quoting.
		sort.Strings(names)
		for i, n := range names {
			names[i] = strconv.Quote(n)
		}
		fmt.Fprintf(&walk, "errs.OnlyMembers(%s%s)\n", p, prefixComma(names))
	}
	if sc.AdditionalProperties != nil {
		if len(sc.Properties) > 0 {
			g.diag(sc.Loc, "additionalProperties next to properties on a Go map is not enforced")
		} else {
			walk.WriteString(g.mapValues(sc.AdditionalProperties, x, mt, p, c))
		}
	}
	b.WriteString(g.memberCount(sc, p, walk.String()))
	if len(sc.OneOf)+len(sc.AnyOf) > 0 {
		g.diag(sc.Loc, "oneOf/anyOf is not enforced on a Go map")
	}
	return b.String()
}

// mapValues checks every value of a map, in key order.
func (g *generator) mapValues(sc *oas.Schema, x string, mt *types.Map, p string, c ctx) string {
	k, v, pk := g.fresh("k"), g.fresh("v"), g.fresh("f")
	nullable := g.isNullable(sc)
	inner := g.value(sc, v, mt.Elem(), pk, c.deeper())
	if inner == "" && nullable {
		return ""
	}
	g.use("slices")
	g.use("maps")
	var body string
	switch {
	case inner == "":
		body = fmt.Sprintf("errs.NotNull(%s.Field(%s))\n", p, k)
	case nullable:
		// A null value satisfies the schema; the checks are for a value.
		body = fmt.Sprintf("if %s := %s.Field(%s); !%s.IsNull() {\n%s%s}\n",
			pk, p, k, pk, bindValue(inner, v, x, k), inner)
	default:
		body = fmt.Sprintf(
			"if %s := %s.Field(%s); errs.NotNull(%s) {\n%s%s}\n",
			pk,
			p,
			k,
			pk,
			bindValue(inner, v, x, k),
			inner,
		)
	}
	return fmt.Sprintf("for _, %s := range slices.Sorted(maps.Keys(%s)) {\n%s%s}\n", k, x, stopWhenFull, body)
}

func bindValue(inner, v, m, k string) string {
	if !uses(inner, v) {
		return ""
	}
	return fmt.Sprintf("%s := %s[%s]\n", v, m, k)
}
