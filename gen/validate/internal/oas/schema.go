package oas

import (
	"math/big"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
)

var annotations = map[string]bool{
	"title": true, "description": true, "example": true, "examples": true, "default": true,
	"deprecated": true, "xml": true, "externalDocs": true, "writeOnly": true, "$comment": true,
	"contentEncoding": true, "contentMediaType": true, "$schema": true, "$id": true, "$anchor": true,
	"discriminator": true, "readOnly": true,
}

var unsupportedKeywords = map[string]bool{
	"not": true, "patternProperties": true, "propertyNames": true, "prefixItems": true,
	"contains": true, "minContains": true, "maxContains": true, "dependentRequired": true,
	"dependentSchemas": true, "if": true, "then": true, "else": true, "unevaluatedProperties": true,
	"unevaluatedItems": true, "$dynamicRef": true, "$defs": true, "contentSchema": true,
}

func (s *Spec) parseSchema(n *yaml.Node, loc string) *Schema {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if sc, ok := s.parsing[n]; ok {
		return sc
	}
	sc := &Schema{Loc: loc}
	s.parsing[n] = sc
	if _, seen := s.lines[loc]; !seen {
		s.lines[loc] = n.Line
	}
	if n.Kind == yaml.ScalarNode {
		// 3.1 boolean schemas: true allows anything, false nothing.
		if n.Value == "false" {
			sc.Unsupported = append(sc.Unsupported, "false schema")
		}
		return sc
	}
	if n.Kind != yaml.MappingNode {
		s.errorf(
			n.Line,
			"write the schema as a mapping, or as true or false in OpenAPI 3.1",
			"%s: schema is not an object",
			loc,
		)
		return sc
	}
	if ref := scalar(get(n, "$ref")); ref != "" {
		sc.Ref = ref
		s.parseRef(ref, loc, n.Line)
		if s.Version == "3.0" {
			return sc // OpenAPI 3.0 ignores the siblings of $ref.
		}
		sib := &Schema{Loc: loc}
		s.fill(sib, n, loc)
		if HasConstraints(sib) {
			sc.siblings = sib
		}
		return sc
	}
	s.fill(sc, n, loc)
	return sc
}

func (s *Spec) parseRef(ref, loc string, line int) {
	if !strings.HasPrefix(ref, "#/components/schemas/") {
		s.errorf(line, "move the schema under #/components/schemas and reference it there",
			"%s: unsupported reference %q", loc, ref)
		return
	}
	if _, done := s.schemas[ref]; done {
		return
	}
	target := s.lookup(ref)
	if target == nil {
		s.errorf(line, unresolvedFix, "%s: unresolved reference %q", loc, ref)
		return
	}
	s.schemas[ref] = nil // mark before recursing, for cycles
	s.schemas[ref] = s.parseSchema(target, ref)
}

func (s *Spec) fill(sc *Schema, n *yaml.Node, loc string) {
	for _, kv := range pairs(n) {
		k, v, kloc := kv.key, kv.val, loc+"/"+validate.EscapeToken(kv.key)
		if s.fillScalar(sc, k, v, kloc) || s.fillCollection(sc, k, v, kloc) || s.fillComposition(sc, k, v, kloc) {
			continue
		}
		switch {
		case k == "$ref" || k == "exclusiveMinimum" || k == "exclusiveMaximum":
			// $ref is read by parseSchema, the exclusive bounds by exclusive.
		case k == WaiverKey:
			s.waive(loc, v)
		case unsupportedKeywords[k]:
			sc.Unsupported = append(sc.Unsupported, k)
		case !annotations[k] && !strings.HasPrefix(k, "x-"):
			sc.Unsupported = append(sc.Unsupported, "unknown keyword "+k)
		}
	}
	s.exclusive(sc, n, loc)
}

// fillScalar reads the keywords of strings, numbers and single values.
func (s *Spec) fillScalar(sc *Schema, k string, v *yaml.Node, loc string) bool {
	switch k {
	case "type":
		fillType(sc, v)
	case "nullable":
		sc.Nullable = sc.Nullable || v.Value == "true"
	case "format":
		sc.Format = v.Value
	case "pattern":
		sc.Pattern = v.Value
	case "minLength":
		sc.MinLength = s.intValue(v, loc)
	case "maxLength":
		sc.MaxLength = s.intValue(v, loc)
	case "minimum":
		sc.Minimum = s.number(v, loc)
	case "maximum":
		sc.Maximum = s.number(v, loc)
	case "multipleOf":
		sc.MultipleOf = s.number(v, loc)
	case "enum":
		sc.Enum = []Value{} // non-nil: an empty enum rejects everything
		for i, e := range v.Content {
			sc.Enum = append(sc.Enum, s.value(e, loc+"/enum/"+strconv.Itoa(i)))
		}
	case "const":
		c := s.value(v, loc+"/const")
		sc.Const = &c
	case "readOnly":
		sc.ReadOnly = v.Value == "true"
	case "x-go-type":
		sc.GoType = v.Value
	default:
		return false
	}
	return true
}

// fillType reads type, a name or (3.1) an array of names; "null" makes the
// schema nullable.
func fillType(sc *Schema, v *yaml.Node) {
	if v.Kind != yaml.SequenceNode {
		if v.Value == "null" {
			sc.Nullable = true
		}
		sc.Types = []string{v.Value}
		return
	}
	for _, t := range v.Content {
		if t.Value == "null" {
			sc.Nullable = true
		} else {
			sc.Types = append(sc.Types, t.Value)
		}
	}
}

// fillCollection reads the keywords of arrays and objects.
func (s *Spec) fillCollection(sc *Schema, k string, v *yaml.Node, loc string) bool {
	switch k {
	case "items":
		if v.Kind == yaml.SequenceNode {
			sc.Unsupported = append(sc.Unsupported, "items as an array (tuple validation)")
		} else {
			sc.Items = s.parseSchema(v, loc)
		}
	case "minItems":
		sc.MinItems = s.intValue(v, loc)
	case "maxItems":
		sc.MaxItems = s.intValue(v, loc)
	case "uniqueItems":
		sc.UniqueItems = v.Value == "true"
	case "properties":
		for _, p := range pairs(v) {
			sc.Properties = append(sc.Properties,
				Property{Name: p.key, Schema: s.parseSchema(p.val, loc+"/"+validate.EscapeToken(p.key))})
		}
	case "required":
		for _, r := range v.Content {
			sc.Required = append(sc.Required, r.Value)
		}
	case "additionalProperties":
		if v.Kind == yaml.ScalarNode {
			sc.AdditionalPropertiesFalse = v.Value == "false"
		} else {
			sc.AdditionalProperties = s.parseSchema(v, loc)
		}
	case "minProperties":
		sc.MinProperties = s.intValue(v, loc)
	case "maxProperties":
		sc.MaxProperties = s.intValue(v, loc)
	default:
		return false
	}
	return true
}

// fillComposition reads allOf, oneOf, anyOf and discriminator.
func (s *Spec) fillComposition(sc *Schema, k string, v *yaml.Node, loc string) bool {
	switch k {
	case "allOf", "oneOf", "anyOf":
		list := make([]*Schema, 0, len(v.Content))
		for i, e := range v.Content {
			list = append(list, s.parseSchema(e, loc+"/"+strconv.Itoa(i)))
		}
		switch k {
		case "allOf":
			sc.AllOf = list
		case "oneOf":
			sc.OneOf = list
		default:
			sc.AnyOf = list
		}
	case "discriminator":
		d := &Discriminator{PropertyName: scalar(get(v, "propertyName"))}
		for _, m := range pairs(get(v, "mapping")) {
			d.Mapping = append(d.Mapping, MappingEntry{Value: m.key, Ref: m.val.Value})
			if strings.HasPrefix(m.val.Value, "#/") {
				s.parseRef(m.val.Value, loc+"/mapping", m.val.Line)
			}
		}
		sc.Discriminator = d
	default:
		return false
	}
	return true
}

func (s *Spec) exclusive(sc *Schema, n *yaml.Node, loc string) {
	for _, side := range []struct {
		key   string
		bound **Number
		excl  **Number
	}{{"exclusiveMinimum", &sc.Minimum, &sc.ExclusiveMinimum}, {"exclusiveMaximum", &sc.Maximum, &sc.ExclusiveMaximum}} {
		v := get(n, side.key)
		if v == nil {
			continue
		}
		if v.Tag == "!!bool" {
			if v.Value == "true" && *side.bound != nil {
				*side.excl, *side.bound = *side.bound, nil
			}
			continue
		}
		*side.excl = s.number(v, loc+"/"+side.key)
	}
}

func HasConstraints(sc *Schema) bool {
	return len(sc.Types) > 0 || sc.Nullable || sc.Format != "" || sc.Pattern != "" || sc.MinLength != nil ||
		sc.MaxLength != nil || sc.Minimum != nil || sc.Maximum != nil || sc.ExclusiveMinimum != nil ||
		sc.ExclusiveMaximum != nil || sc.MultipleOf != nil || sc.Enum != nil || sc.Const != nil ||
		sc.Items != nil || sc.MinItems != nil || sc.MaxItems != nil || sc.UniqueItems ||
		len(sc.Properties) > 0 || len(sc.Required) > 0 || sc.AdditionalProperties != nil ||
		sc.AdditionalPropertiesFalse || sc.MinProperties != nil || sc.MaxProperties != nil ||
		len(sc.AllOf) > 0 || len(sc.OneOf) > 0 || len(sc.AnyOf) > 0 || len(sc.Unsupported) > 0
}

func (s *Spec) intValue(n *yaml.Node, loc string) *int {
	i, err := strconv.Atoi(n.Value)
	if err != nil || i < 0 {
		s.errorf(n.Line, "write a non-negative integer", "%s: want a non-negative integer, got %q", loc, n.Value)
		return nil
	}
	return &i
}

func (s *Spec) number(n *yaml.Node, loc string) *Number {
	r, ok := new(big.Rat).SetString(n.Value)
	if !ok || n.Kind != yaml.ScalarNode {
		s.errorf(n.Line, "write a number", "%s: want a number, got %q", loc, n.Value)
		return nil
	}
	return &Number{Lit: n.Value, Rat: r}
}

// value reads an enum or const value. Generated code compares against it, so
// a boolean or a number is decoded, never copied: Str holds the canonical
// literal ("true", or the number as written once it parsed). A node whose
// explicit tag does not fit its text, such as !!bool "x()", is a spec error.
func (s *Spec) value(n *yaml.Node, loc string) Value {
	if n.Kind != yaml.ScalarNode {
		return Value{Kind: "other"}
	}
	switch n.Tag {
	case "!!null":
		return Value{Kind: "null"}
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			s.errorf(n.Line, "write true or false, or drop the !!bool tag", "%s: %q is not a boolean", loc, n.Value)
			return Value{Kind: "other"}
		}
		return Value{Kind: "boolean", Str: strconv.FormatBool(b)}
	case "!!int", "!!float":
		if _, ok := new(big.Rat).SetString(n.Value); !ok {
			s.errorf(n.Line, "write a number, or drop the tag", "%s: %q is not a number", loc, n.Value)
			return Value{Kind: "other"}
		}
		return Value{Kind: "number", Str: n.Value}
	}
	return Value{Kind: "string", Str: n.Value}
}

type kv struct {
	key string
	val *yaml.Node
}

func pairs(n *yaml.Node) []kv {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]kv, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		v := n.Content[i+1]
		if v.Kind == yaml.AliasNode {
			v = v.Alias
		}
		out = append(out, kv{n.Content[i].Value, v})
	}
	return out
}

func get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			v := n.Content[i+1]
			if v.Kind == yaml.AliasNode {
				v = v.Alias
			}
			return v
		}
	}
	return nil
}

func scalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// lineOf returns the line of n in the spec, 0 for a missing node.
func lineOf(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	return n.Line
}
