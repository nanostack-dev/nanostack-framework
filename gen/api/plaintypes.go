package genapi

import (
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// The oapi-codegen extensions PlainTypes removes.
const (
	extGoType            = "x-go-type"
	extGoTypeImport      = "x-go-type-import"
	extSkipOptionalPtr   = "x-go-type-skip-optional-pointer"
	nullableGoTypePrefix = "nullable."
)

// plainTypes removes the server's Go type hints from doc, so a client
// generated from it uses plain types. In every extension map of the
// document, on schemas, schema references, parameters, headers and media
// types:
//
//   - x-go-type is deleted, unless it is "string";
//   - x-go-type-import is deleted;
//   - x-go-type-skip-optional-pointer is deleted when the x-go-type next to
//     it names the nullable package (nullable.Nullable[T]): the server tells
//     an omitted field from null, and the client keeps the pointer that
//     sends null.
//
// It walks the parsed document, so a hint in a file that a $ref names is
// removed too. A schema that several references share is cleaned once.
func plainTypes(doc *openapi3.T) {
	w := plainWalker{seen: map[*openapi3.Schema]bool{}}
	w.doc(doc)
}

type plainWalker struct {
	seen map[*openapi3.Schema]bool
}

// ext removes the hints from one extension map.
func (plainWalker) ext(m map[string]any) {
	if m == nil {
		return
	}
	if t, ok := m[extGoType]; ok {
		s, isString := t.(string)
		if !isString || s != "string" {
			delete(m, extGoType)
		}
		if isString && strings.HasPrefix(s, nullableGoTypePrefix) {
			delete(m, extSkipOptionalPtr)
		}
	}
	delete(m, extGoTypeImport)
}

func (w plainWalker) doc(doc *openapi3.T) {
	w.ext(doc.Extensions)
	if c := doc.Components; c != nil {
		for _, s := range c.Schemas {
			w.schema(s)
		}
		for _, p := range c.Parameters {
			w.parameter(p)
		}
		for _, h := range c.Headers {
			w.header(h)
		}
		for _, b := range c.RequestBodies {
			w.requestBody(b)
		}
		for _, r := range c.Responses {
			w.response(r)
		}
		for _, cb := range c.Callbacks {
			w.callback(cb)
		}
	}
	if doc.Paths != nil {
		for _, item := range doc.Paths.Map() {
			w.pathItem(item)
		}
	}
	for _, item := range doc.Webhooks {
		w.pathItem(item)
	}
}

func (w plainWalker) pathItem(item *openapi3.PathItem) {
	if item == nil {
		return
	}
	for _, p := range item.Parameters {
		w.parameter(p)
	}
	for _, op := range item.Operations() {
		w.operation(op)
	}
}

func (w plainWalker) operation(op *openapi3.Operation) {
	if op == nil {
		return
	}
	for _, p := range op.Parameters {
		w.parameter(p)
	}
	w.requestBody(op.RequestBody)
	if op.Responses != nil {
		for _, r := range op.Responses.Map() {
			w.response(r)
		}
	}
	for _, cb := range op.Callbacks {
		w.callback(cb)
	}
}

func (w plainWalker) callback(ref *openapi3.CallbackRef) {
	if ref == nil || ref.Value == nil {
		return
	}
	for _, item := range ref.Value.Map() {
		w.pathItem(item)
	}
}

func (w plainWalker) parameter(ref *openapi3.ParameterRef) {
	if ref == nil || ref.Value == nil {
		return
	}
	w.ext(ref.Value.Extensions)
	w.schema(ref.Value.Schema)
	w.content(ref.Value.Content)
}

func (w plainWalker) header(ref *openapi3.HeaderRef) {
	if ref == nil || ref.Value == nil {
		return
	}
	w.ext(ref.Value.Extensions)
	w.schema(ref.Value.Schema)
	w.content(ref.Value.Content)
}

func (w plainWalker) requestBody(ref *openapi3.RequestBodyRef) {
	if ref == nil || ref.Value == nil {
		return
	}
	w.content(ref.Value.Content)
}

func (w plainWalker) response(ref *openapi3.ResponseRef) {
	if ref == nil || ref.Value == nil {
		return
	}
	for _, h := range ref.Value.Headers {
		w.header(h)
	}
	w.content(ref.Value.Content)
}

func (w plainWalker) content(c openapi3.Content) {
	for _, mt := range c {
		if mt == nil {
			continue
		}
		w.ext(mt.Extensions)
		w.schema(mt.Schema)
		w.schema(mt.ItemSchema)
		for _, enc := range mt.Encoding {
			if enc == nil {
				continue
			}
			for _, h := range enc.Headers {
				w.header(h)
			}
		}
	}
}

// schema cleans a schema reference, its schema and every schema under it.
func (w plainWalker) schema(ref *openapi3.SchemaRef) {
	if ref == nil {
		return
	}
	w.ext(ref.Extensions)
	s := ref.Value
	if s == nil || w.seen[s] {
		return
	}
	w.seen[s] = true
	w.ext(s.Extensions)
	for _, refs := range []openapi3.SchemaRefs{s.OneOf, s.AnyOf, s.AllOf, s.PrefixItems} {
		for _, r := range refs {
			w.schema(r)
		}
	}
	for _, m := range []openapi3.Schemas{s.Properties, s.PatternProperties, s.DependentSchemas, s.Defs} {
		for _, r := range m {
			w.schema(r)
		}
	}
	for _, r := range []*openapi3.SchemaRef{
		s.Not, s.Items, s.AdditionalProperties.Schema, s.Contains, s.PropertyNames,
		s.UnevaluatedItems.Schema, s.UnevaluatedProperties.Schema, s.If, s.Then, s.Else, s.ContentSchema,
	} {
		w.schema(r)
	}
}
