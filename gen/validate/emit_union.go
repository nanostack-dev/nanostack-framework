package genvalidate

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// singleVariant returns X for a oneOf or anyOf of X and null.
func (g *generator) singleVariant(sc *Schema) *Schema {
	var only *Schema
	for _, v := range append(slices.Clone(sc.OneOf), sc.AnyOf...) {
		r := g.resolve(v)
		if len(r.Types) == 1 && r.Types[0] == "null" {
			continue
		}
		if only != nil {
			return nil
		}
		only = v
	}
	return only
}

// unionWithVariants reports whether t is an oapi-codegen union with n
// variants.
func (g *generator) unionWithVariants(t types.Type, n int) bool {
	st, ok := structOf(t)
	if !ok || !isUnion(st) {
		return false
	}
	named := g.localNamed(t)
	return named != nil && len(asMethods(named)) == n
}

func isUnion(st *types.Struct) bool {
	for f := range st.Fields() {
		if f.Name() == "union" && types.TypeString(f.Type(), nil) == "encoding/json.RawMessage" {
			return true
		}
	}
	return false
}

// union checks an oapi-codegen union type against its oneOf or anyOf.
func (g *generator) union(sc *Schema, x string, t types.Type, p string, c ctx) string {
	variants, code, verb := sc.OneOf, "validate.CodeOneOf", "exactly one"
	if len(variants) == 0 {
		variants, code, verb = sc.AnyOf, "validate.CodeAnyOf", "at least one"
	}
	if len(variants) == 0 {
		return ""
	}
	if len(sc.OneOf) > 0 && len(sc.AnyOf) > 0 {
		g.diag(sc.Loc, "anyOf next to oneOf is not enforced")
	}
	n := g.localNamed(t)
	if n == nil {
		g.diag(sc.Loc, "oneOf/anyOf is not enforced on Go type "+types.TypeString(t, nil))
		return ""
	}
	goVariants := asMethods(n)
	var nonNull []*Schema
	for _, v := range variants {
		r := g.resolve(v)
		if len(r.Types) == 1 && r.Types[0] == "null" {
			continue
		}
		nonNull = append(nonNull, v)
	}
	if len(goVariants) != len(nonNull) {
		g.diag(
			sc.Loc,
			fmt.Sprintf(
				"oneOf/anyOf is not enforced: Go union %s has %d variants, the schema %d",
				n.Obj().Name(),
				len(goVariants),
				len(nonNull),
			),
		)
		return ""
	}
	names := make([]string, len(nonNull))
	for i, v := range nonNull {
		if v.Ref != "" {
			names[i] = RefName(v.Ref)
		} else {
			names[i] = types.TypeString(goVariants[i], func(*types.Package) string { return "" })
		}
	}
	if d := sc.Discriminator; d != nil && d.PropertyName != "" {
		return g.discriminated(d, nonNull, goVariants, x, p, c)
	}
	var b strings.Builder
	matched := g.fresh("matched")
	fmt.Fprintf(&b, "%s := 0\n", matched)
	for i, v := range nonNull {
		val := g.fresh("v")
		inner := g.value(v, val, goVariants[i], p, c.deeper())
		if inner == "" {
			fmt.Fprintf(
				&b,
				"if _, verrs := validate.Variant[%s](%s.union, %s); verrs == nil {\n%s++\n}\n",
				g.typeString(goVariants[i]),
				x,
				p,
				matched,
			)
			continue
		}
		fmt.Fprintf(
			&b,
			"if %s, verrs := validate.Variant[%s](%s.union, %s); verrs == nil {\nvar errs validate.Errors\n%sif len(errs) == 0 {\n%s++\n}\n}\n",
			bindOrBlank(inner, val),
			g.typeString(goVariants[i]),
			x,
			p,
			inner,
			matched,
		)
	}
	list := strings.Join(names, ", ")
	fmt.Fprintf(&b, "if %s == 0 {\nerrs.Add(%s, %s, %q)\n}", matched, p, code, "must match "+verb+" of: "+list)
	if code == "validate.CodeOneOf" {
		fmt.Fprintf(
			&b,
			" else if %s > 1 {\nerrs.Add(%s, %s, %q)\n}",
			matched,
			p,
			code,
			"must match exactly one of: "+list+", but matches several",
		)
	}
	b.WriteString("\n")
	return b.String()
}

func (g *generator) discriminated(
	d *Discriminator,
	variants []*Schema,
	goVariants []types.Type,
	x, p string,
	c ctx,
) string {
	values := g.discriminatorValues(d, variants)
	var all []string
	var b strings.Builder
	dv := g.fresh("d")
	fmt.Fprintf(&b, "if %s, ok := errs.Discriminator(%s.union, %s, %q); ok {\nswitch %s {\n",
		dv, x, p, d.PropertyName, dv)
	for i, v := range variants {
		if len(values[i]) == 0 {
			g.diag(v.Loc, "variant cannot be selected by the discriminator and is not checked")
			continue
		}
		quoted := make([]string, len(values[i]))
		for j, s := range values[i] {
			quoted[j] = strconv.Quote(s)
		}
		all = append(all, values[i]...)
		fmt.Fprintf(&b, "case %s:\n%s", strings.Join(quoted, ", "), g.variantCase(v, goVariants[i], x, p, c))
	}
	fmt.Fprintf(&b, "default:\nerrs.Add(%s.Field(%q), validate.CodeDiscriminator, %q)\n}\n}\n",
		p, d.PropertyName, "must be one of: "+strings.Join(all, ", "))
	return b.String()
}

// variantCase decodes the selected variant strictly and validates it.
func (g *generator) variantCase(v *Schema, t types.Type, x, p string, c ctx) string {
	val := g.fresh("v")
	inner := g.value(v, val, t, p, c.deeper())
	if inner == "" {
		return fmt.Sprintf(
			"_, verrs := validate.Variant[%s](%s.union, %s)\nerrs.Append(verrs)\n",
			g.typeString(t),
			x,
			p,
		)
	}
	return fmt.Sprintf("%s, verrs := validate.Variant[%s](%s.union, %s)\nerrs.Append(verrs)\nif verrs == nil {\n%s}\n",
		bindOrBlank(inner, val), g.typeString(t), x, p, inner)
}

// discriminatorValues lists, per variant, the discriminator values that
// select it: the explicit mapping, then the component name of each $ref
// variant the mapping does not name.
func (g *generator) discriminatorValues(d *Discriminator, variants []*Schema) [][]string {
	values := make([][]string, len(variants))
	mapped := map[string]bool{}
	for _, m := range d.Mapping {
		target := m.Ref
		if !strings.HasPrefix(target, "#/") {
			target = "#/components/schemas/" + target
		}
		found := false
		for i, v := range variants {
			if v.Ref == target {
				values[i] = append(values[i], m.Value)
				mapped[target], found = true, true
			}
		}
		if !found {
			g.diag(variants[0].Loc, "discriminator mapping "+m.Value+" names a schema outside the oneOf/anyOf")
		}
	}
	for i, v := range variants {
		if v.Ref != "" && !mapped[v.Ref] {
			values[i] = append(values[i], RefName(v.Ref))
		}
	}
	return values
}

// asMethods returns the variant types of an oapi-codegen union, in the
// order of its As<Variant> methods in the source, which is the schema order.
func asMethods(n *types.Named) []types.Type {
	type variant struct {
		pos token.Pos
		t   types.Type
	}
	var vs []variant
	for f := range n.Methods() {
		if !strings.HasPrefix(f.Name(), "As") {
			continue
		}
		sig, isSig := f.Type().(*types.Signature)
		if !isSig || sig.Params().Len() != 0 || sig.Results().Len() != 2 ||
			sig.Results().At(1).Type().String() != "error" {
			continue
		}
		vs = append(vs, variant{f.Pos(), sig.Results().At(0).Type()})
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].pos < vs[j].pos })
	out := make([]types.Type, len(vs))
	for i, v := range vs {
		out[i] = v.t
	}
	return out
}
