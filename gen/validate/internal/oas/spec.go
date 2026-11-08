// Package oas is the model of an OpenAPI 3.0 or 3.1 document that nano gen
// validate reads: operations, request bodies, parameters and schemas. It is
// internal so the generator can change it without breaking an importer.
package oas

import (
	"errors"
	"math/big"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/gen"
)

// Spec is the part of an OpenAPI 3.0 or 3.1 document the generator reads:
// operations, their request bodies and parameters, and component schemas.
// Only local references (#/components/...) are supported.
type Spec struct {
	Path       string
	Version    string // "3.0" or "3.1"
	Operations []*Operation

	root    *yaml.Node
	schemas map[string]*Schema
	parsing map[*yaml.Node]*Schema
	errs    []error
}

// Operation is one method on one path.
type Operation struct {
	Method string // upper case
	Path   string
	ID     string
	Body   *RequestBody
	Params []*Parameter
	Loc    string
}

// RequestBody is an operation's request body.
type RequestBody struct {
	Required bool
	Content  []MediaType
}

// MediaType is one entry of a request body's content map.
type MediaType struct {
	Type   string
	Schema *Schema
}

// Parameter is a path, query, header or cookie parameter.
type Parameter struct {
	Name     string
	In       string
	Required bool
	Schema   *Schema
	Loc      string
}

// Schema is a JSON Schema as OpenAPI uses it, normalised across 3.0 and 3.1:
// nullable and type arrays both set Nullable, and the 3.0 boolean exclusive
// bounds become ExclusiveMinimum and ExclusiveMaximum.
type Schema struct {
	Loc string
	Ref string

	Types    []string // JSON types other than "null"
	Nullable bool

	Format    string
	Pattern   string
	MinLength *int
	MaxLength *int

	Minimum          *Number
	Maximum          *Number
	ExclusiveMinimum *Number
	ExclusiveMaximum *Number
	MultipleOf       *Number

	Enum  []Value
	Const *Value

	Items       *Schema
	MinItems    *int
	MaxItems    *int
	UniqueItems bool

	Properties                []Property
	Required                  []string
	AdditionalProperties      *Schema
	AdditionalPropertiesFalse bool
	MinProperties             *int
	MaxProperties             *int

	AllOf         []*Schema
	OneOf         []*Schema
	AnyOf         []*Schema
	Discriminator *Discriminator

	ReadOnly bool
	GoType   string // x-go-type

	// Unsupported lists keywords present in the schema that the generator
	// does not enforce.
	Unsupported []string

	// siblings is set for a 3.1 $ref with constraint siblings.
	siblings *Schema
}

// Property is a named member of an object schema.
type Property struct {
	Name   string
	Schema *Schema
}

// Discriminator selects a oneOf or anyOf branch by a property.
type Discriminator struct {
	PropertyName string
	Mapping      []MappingEntry
}

// MappingEntry maps a discriminator value to a schema reference.
type MappingEntry struct {
	Value string
	Ref   string
}

// Number is a numeric literal from the spec.
type Number struct {
	Lit string
	Rat *big.Rat
}

// IsInt reports whether the number is an integer.
func (n *Number) IsInt() bool { return n.Rat.IsInt() }

// Value is a scalar enum or const value.
type Value struct {
	Kind string // "string", "number", "boolean", "null", "other"
	Str  string // the string, or the literal for numbers and booleans
}

// LoadSpec reads and parses an OpenAPI document.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, gen.Errorf(path, 0, "pass -spec with the path of the OpenAPI document", "the file does not exist")
	}
	if err != nil {
		return nil, &gen.Error{File: path, Msg: err.Error(), Fix: "make the file readable", Err: gen.ErrEnvironment}
	}
	return ParseSpec(path, data)
}

// ParseSpec parses an OpenAPI document. path is used in messages.
func ParseSpec(path string, data []byte) (*Spec, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, gen.Errorf(path, 0, "fix the YAML syntax", "%v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, gen.Errorf(path, 0, "pass -spec with an OpenAPI 3.0 or 3.1 document", "not an OpenAPI document")
	}
	s := &Spec{Path: path, root: doc.Content[0], schemas: map[string]*Schema{}, parsing: map[*yaml.Node]*Schema{}}
	version := scalar(get(s.root, "openapi"))
	switch {
	case strings.HasPrefix(version, "3.0."):
		s.Version = "3.0"
	case strings.HasPrefix(version, "3.1."):
		s.Version = "3.1"
	default:
		return nil, gen.Errorf(path, lineOf(get(s.root, "openapi")), "set openapi: to 3.0.x or 3.1.x",
			"unsupported openapi version %q", version)
	}
	s.parseOperations()
	if len(s.errs) > 0 {
		return nil, JoinErrors(s.errs)
	}
	return s, nil
}

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

func (s *Spec) parseOperations() {
	paths := get(s.root, "paths")
	for _, kv := range pairs(paths) {
		path, item := kv.key, kv.val
		itemLoc := "#/paths/" + validate.EscapeToken(path)
		shared := s.parseParams(get(item, "parameters"), itemLoc+"/parameters")
		for _, m := range methods {
			opNode := get(item, m)
			if opNode == nil {
				continue
			}
			loc := itemLoc + "/" + m
			op := &Operation{Method: strings.ToUpper(m), Path: path, ID: scalar(get(opNode, "operationId")), Loc: loc}
			own := s.parseParams(get(opNode, "parameters"), loc+"/parameters")
			op.Params = mergeParams(shared, own)
			if rb := get(opNode, "requestBody"); rb != nil {
				op.Body = s.parseRequestBody(rb, loc+"/requestBody")
			}
			s.Operations = append(s.Operations, op)
		}
	}
}

func mergeParams(shared, own []*Parameter) []*Parameter {
	out := slices.Clone(own)
	for _, p := range shared {
		if !slices.ContainsFunc(own, func(o *Parameter) bool { return o.Name == p.Name && o.In == p.In }) {
			out = append(out, p)
		}
	}
	return out
}

func (s *Spec) parseParams(n *yaml.Node, loc string) []*Parameter {
	var out []*Parameter
	if n == nil {
		return nil
	}
	for i, pn := range n.Content {
		ploc := loc + "/" + strconv.Itoa(i)
		pn, ploc = s.deref(pn, ploc)
		if pn == nil {
			continue
		}
		p := &Parameter{
			Name:     scalar(get(pn, "name")),
			In:       scalar(get(pn, "in")),
			Required: scalar(get(pn, "required")) == "true",
			Loc:      ploc,
		}
		if sn := get(pn, "schema"); sn != nil {
			p.Schema = s.parseSchema(sn, ploc+"/schema")
		} else if c := get(pn, "content"); c != nil {
			for _, kv := range pairs(c) {
				if csn := get(kv.val, "schema"); csn != nil {
					p.Schema = s.parseSchema(csn, ploc+"/content/"+validate.EscapeToken(kv.key)+"/schema")
				}
			}
		}
		out = append(out, p)
	}
	return out
}

func (s *Spec) parseRequestBody(n *yaml.Node, loc string) *RequestBody {
	n, loc = s.deref(n, loc)
	if n == nil {
		return nil
	}
	rb := &RequestBody{Required: scalar(get(n, "required")) == "true"}
	for _, kv := range pairs(get(n, "content")) {
		mt := MediaType{Type: kv.key}
		if sn := get(kv.val, "schema"); sn != nil {
			mt.Schema = s.parseSchema(sn, loc+"/content/"+validate.EscapeToken(kv.key)+"/schema")
		}
		rb.Content = append(rb.Content, mt)
	}
	return rb
}

// deref follows a $ref to components (requestBodies, parameters).
func (s *Spec) deref(n *yaml.Node, loc string) (*yaml.Node, string) {
	for i := 0; n != nil && i < 16; i++ {
		ref := scalar(get(n, "$ref"))
		if ref == "" {
			return n, loc
		}
		target := s.lookup(ref)
		if target == nil {
			s.errorf(n.Line, unresolvedFix, "%s: unresolved reference %q", loc, ref)
			return nil, loc
		}
		n, loc = target, ref
	}
	return n, loc
}

// lookup resolves a local JSON Pointer reference.
func (s *Spec) lookup(ref string) *yaml.Node {
	ptr, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil
	}
	n := s.root
	for tok := range strings.SplitSeq(ptr, "/") {
		tok = validate.UnescapeToken(tok)
		if n = get(n, tok); n == nil {
			return nil
		}
	}
	return n
}

// Resolve follows $ref chains to the target schema. A 3.1 $ref with
// constraint siblings resolves to itself; callers handle the siblings.
func (s *Spec) Resolve(sc *Schema) *Schema {
	for i := 0; sc != nil && sc.Ref != "" && sc.siblings == nil && i < 32; i++ {
		sc = s.schemas[sc.Ref]
	}
	return sc
}

// Siblings returns the constraints next to a 3.1 $ref, or nil.
func (sc *Schema) Siblings() *Schema { return sc.siblings }

// RefName returns the component name of a schema reference.
func RefName(ref string) string {
	return ref[strings.LastIndexByte(ref, '/')+1:]
}

// unresolvedFix is the fix for a $ref that points nowhere.
const unresolvedFix = "define the referenced component, or correct the $ref"

// errorf records a problem in the spec at line, 0 when unknown.
func (s *Spec) errorf(line int, fix, format string, args ...any) {
	s.errs = append(s.errs, gen.Errorf(s.Path, line, fix, format, args...))
}

// JoinErrors joins problems without duplicates, sorted by their text, so the
// report does not depend on map order.
func JoinErrors(errs []error) error {
	seen := map[string]bool{}
	out := make([]error, 0, len(errs))
	for _, e := range errs {
		if !seen[e.Error()] {
			seen[e.Error()] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Error() < out[j].Error() })
	return errors.Join(out...)
}
