package genrepo

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// schemaRef is the prefix of a reference to a schema of the spec.
const schemaRef = "#/components/schemas/"

// apiSpec is the OpenAPI document, kept as YAML nodes so that a problem
// names the line of the spec. nano gen repo reads what the both-ways check
// needs (design S13): the request body schemas, their properties after
// allOf, the sort_by enum, and the limits.
type apiSpec struct {
	rel     string                // the path, relative to the package directory, for messages
	schemas map[string]*yaml.Node // components.schemas, by name
	bodies  map[string][]string   // schema name -> the operationIds whose JSON request body it is
}

// apiProp is one property of a schema, after allOf.
type apiProp struct {
	name     string
	schema   *yaml.Node // the property schema, its $ref followed
	line     int
	own      bool // declared by the schema itself or an inline allOf part, not through a $ref
	required bool
}

// loadSpec reads the spec at path. rel names it in messages.
func loadSpec(path, rel string) (*apiSpec, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, gen.EnvErrorf("set API.Spec in nanogen/project.go to the OpenAPI document",
			"read the OpenAPI spec: %v", err)
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil, gen.Errorf(rel, 0, "fix the YAML of the spec", "the OpenAPI spec does not parse: %v", err)
	}
	root := doc.Content[0]
	s := &apiSpec{rel: rel, schemas: map[string]*yaml.Node{}, bodies: map[string][]string{}}
	if schemas := child(child(root, "components"), "schemas"); schemas != nil {
		for i := 0; i+1 < len(schemas.Content); i += 2 {
			s.schemas[schemas.Content[i].Value] = schemas.Content[i+1]
		}
	}
	paths := child(root, "paths")
	for i := 1; paths != nil && i < len(paths.Content); i += 2 {
		item := paths.Content[i]
		for j := 1; j < len(item.Content); j += 2 {
			op := item.Content[j]
			schema := child(child(child(child(op, "requestBody"), "content"), "application/json"), "schema")
			ref, id := child(schema, "$ref"), child(op, "operationId")
			if ref != nil && id != nil && strings.HasPrefix(ref.Value, schemaRef) {
				name := strings.TrimPrefix(ref.Value, schemaRef)
				s.bodies[name] = append(s.bodies[name], id.Value)
			}
		}
	}
	return s, nil
}

// child returns the value of key in the mapping n, or nil.
func child(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// resolve follows the $ref of n to a schema of the spec. It returns nil
// for a reference that leaves the document or names no schema: the check
// then reads the property as having no schema.
func (s *apiSpec) resolve(n *yaml.Node) *yaml.Node {
	for range 32 { // a cycle of references is not a schema
		ref := child(n, "$ref")
		if ref == nil {
			return n
		}
		name, local := strings.CutPrefix(ref.Value, schemaRef)
		if n = s.schemas[name]; !local || n == nil {
			return nil
		}
	}
	return nil
}

// props returns the properties of schema n after allOf: its own
// properties, then those of each allOf part, in order. A property reached
// through a $ref is inherited; one in the schema or in an inline part is
// the schema's own. unresolved holds each $ref on the way that names no
// schema of the document: the properties behind it are unknown.
func (s *apiSpec) props(n *yaml.Node) (out []apiProp, unresolved []*yaml.Node) {
	w := &propWalk{s: s, seen: map[string]bool{}}
	w.walk(n, true, 0)
	return w.out, w.unresolved
}

// propWalk collects the properties of a schema and its allOf parts.
type propWalk struct {
	s          *apiSpec
	seen       map[string]bool
	out        []apiProp
	unresolved []*yaml.Node
}

func (w *propWalk) walk(n *yaml.Node, own bool, depth int) {
	if ref := child(n, "$ref"); ref != nil {
		own = false
		if w.s.resolve(n) == nil {
			w.unresolved = append(w.unresolved, ref)
		}
	}
	if n = w.s.resolve(n); n == nil || depth > 16 {
		return
	}
	var required []string
	for _, r := range contentOf(child(n, "required")) {
		required = append(required, r.Value)
	}
	props := contentOf(child(n, "properties"))
	for i := 0; i+1 < len(props); i += 2 {
		if name := props[i].Value; !w.seen[name] {
			w.seen[name] = true
			w.out = append(w.out, apiProp{name: name, schema: w.s.resolve(props[i+1]), line: props[i].Line, own: own})
		}
	}
	for i := range w.out {
		w.out[i].required = w.out[i].required || slices.Contains(required, w.out[i].name)
	}
	for _, part := range contentOf(child(n, "allOf")) {
		w.walk(part, own, depth+1)
	}
}

// contentOf returns the content of n, nil for a nil node.
func contentOf(n *yaml.Node) []*yaml.Node {
	if n == nil {
		return nil
	}
	return n.Content
}

// prop returns the property name of props, or nil.
func prop(props []apiProp, name string) *apiProp {
	for i := range props {
		if props[i].name == name {
			return &props[i]
		}
	}
	return nil
}

// enum returns the string values of the enum of n, after allOf, and the
// line of the enum. ok is false when n has no enum.
func (s *apiSpec) enum(n *yaml.Node) (values []string, line int, ok bool) {
	if n = s.resolve(n); n == nil {
		return nil, 0, false
	}
	if e := child(n, "enum"); e != nil {
		for _, v := range e.Content {
			values = append(values, v.Value)
		}
		return values, e.Line, true
	}
	if all := child(n, "allOf"); all != nil {
		for _, part := range all.Content {
			if values, line, ok = s.enum(part); ok {
				return values, line, true
			}
		}
	}
	return nil, 0, false
}

// number returns the integer value of key in schema n, after allOf, and
// its line. ok is false when n does not set key to an integer.
func (s *apiSpec) number(n *yaml.Node, key string) (value int64, line int, ok bool) {
	if n = s.resolve(n); n == nil {
		return 0, 0, false
	}
	if v := child(n, key); v != nil {
		parsed, err := strconv.ParseInt(v.Value, 10, 64)
		return parsed, v.Line, err == nil
	}
	if all := child(n, "allOf"); all != nil {
		for _, part := range all.Content {
			if value, line, ok = s.number(part, key); ok {
				return value, line, true
			}
		}
	}
	return 0, 0, false
}

// items returns the items schema of the array schema n, or nil.
func (s *apiSpec) items(n *yaml.Node) *yaml.Node {
	if n = s.resolve(n); n == nil {
		return nil
	}
	if it := child(n, "items"); it != nil {
		return s.resolve(it)
	}
	if all := child(n, "allOf"); all != nil {
		for _, part := range all.Content {
			if it := s.items(part); it != nil {
				return it
			}
		}
	}
	return nil
}
