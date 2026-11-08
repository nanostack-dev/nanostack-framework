package genapi

import (
	"errors"
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// The tag rules of the api step (ADR 0024). A tag is a code boundary: the
// operations of one tag form one interface, served by one handler.

// tagGroup is one tag of the spec and the Go names it becomes.
type tagGroup struct {
	tag   string // as the spec writes it
	iface string // the interface, such as EmailServer
	field string // the Handlers field and NewHandlers parameter, such as email
	pos   position
	ops   []codegen.OperationDefinition
}

// specOp is one operation of the spec, where it is written.
type specOp struct {
	op     *openapi3.Operation
	name   string // the operationId, or "METHOD /path" without one
	pos    position
	tagPos []position // the position of each tag in the operation
}

// position is a line of the spec, or of a file a $ref brought in.
type position struct {
	file string
	line int
}

// posError is a problem and where it is, to sort problems by position.
type posError struct {
	pos position
	err *gen.Error
}

// tagCheck collects the tags of a spec and the problems of the tag rules.
type tagCheck struct {
	spec     string
	declared map[string]bool
	names    []string // declared, in spec order
	byTag    map[string]*tagGroup
	order    []string // tags in the order they were found: declared first, then used
	uses     map[string]int
	errs     []posError
}

func (c *tagCheck) add(pos position, fix, format string, args ...any) {
	c.errs = append(c.errs, posError{pos, gen.Errorf(pos.file, pos.line, fix, format, args...)})
}

// group adds the group of tag at pos, unless it has one.
func (c *tagCheck) group(tag string, pos position) {
	if _, ok := c.byTag[tag]; !ok {
		c.byTag[tag] = &tagGroup{tag: tag, pos: pos}
		c.order = append(c.order, tag)
	}
}

// tagGroups checks the tag rules of the spec and returns one group per tag,
// sorted by interface name. Every operation has exactly one tag; every tag
// an operation uses is declared under the top-level tags, and every declared
// tag is used; each tag becomes a distinct Go name. Each problem is an error
// at the spec line that holds it.
func tagGroups(doc *openapi3.T, spec string) ([]*tagGroup, error) {
	c := &tagCheck{spec: spec, declared: map[string]bool{}, byTag: map[string]*tagGroup{}, uses: map[string]int{}}
	c.declare(doc.Tags)
	for _, o := range specOperations(doc, spec) {
		c.operation(o)
	}
	c.usage()
	c.goNames()
	if len(c.errs) > 0 {
		slices.SortStableFunc(c.errs, func(a, b posError) int {
			if n := strings.Compare(a.pos.file, b.pos.file); n != 0 {
				return n
			}
			return a.pos.line - b.pos.line
		})
		out := make([]error, len(c.errs))
		for i, e := range c.errs {
			out[i] = e.err
		}
		return nil, errors.Join(out...)
	}
	groups := make([]*tagGroup, 0, len(c.order))
	for _, tag := range c.order {
		groups = append(groups, c.byTag[tag])
	}
	slices.SortFunc(groups, func(a, b *tagGroup) int { return strings.Compare(a.iface, b.iface) })
	return groups, nil
}

// declare reads the top-level tags.
func (c *tagCheck) declare(tags openapi3.Tags) {
	for _, t := range tags {
		if t == nil {
			continue
		}
		pos := originPos(t.Origin, c.spec)
		if c.declared[t.Name] {
			c.add(pos, "delete one of them", "tag %s is declared twice", t.Name)
			continue
		}
		c.declared[t.Name] = true
		c.names = append(c.names, t.Name)
		c.group(t.Name, pos)
	}
}

// operation checks that o has one tag and counts the tags it uses.
func (c *tagCheck) operation(o specOp) {
	switch len(o.op.Tags) {
	case 0:
		fix := "add a tag to the operation, and declare it under the top-level tags"
		if len(c.names) > 0 {
			fix = "add one of the declared tags: " + strings.Join(c.names, ", ")
		}
		c.add(o.pos, fix, "operation %s has no tag", o.name)
	case 1:
	default:
		c.add(o.tagPos[0], "keep one tag", "operation %s has %d tags (%s); a tag is a handler boundary",
			o.name, len(o.op.Tags), strings.Join(o.op.Tags, ", "))
	}
	for i, tag := range o.op.Tags {
		c.uses[tag]++
		c.group(tag, o.tagPos[i])
	}
}

// usage reports a used tag that is not declared and a declared tag that is
// not used.
func (c *tagCheck) usage() {
	for _, tag := range c.order {
		g := c.byTag[tag]
		switch {
		case !c.declared[tag]:
			c.add(g.pos, fmt.Sprintf("add %q under the top-level tags", "- name: "+tag),
				"tag %s is used by %s but not declared", tag, plural(c.uses[tag], "operation"))
		case c.uses[tag] == 0:
			c.add(g.pos, "delete it, or tag an operation with it", "tag %s is declared but no operation uses it", tag)
		}
	}
}

// goNames sets the Go names of each group and reports a tag without a
// usable name and two tags with the same one.
func (c *tagCheck) goNames() {
	ifaces := map[string]string{} // interface -> tag
	fields := map[string]string{} // field -> tag
	for _, tag := range c.order {
		g := c.byTag[tag]
		base := codegen.ToCamelCase(tag)
		r, _ := utf8.DecodeRuneInString(base)
		if base == "" || !unicode.IsUpper(r) || !token.IsIdentifier(base) {
			c.add(g.pos, "rename the tag so that it starts with a letter",
				"tag %q has no Go name: it becomes %q", tag, base+"Server")
			continue
		}
		g.iface, g.field = base+"Server", fieldName(base)
		if other, dup := ifaces[g.iface]; dup {
			c.add(g.pos, "rename one tag", "tags %q and %q both become %s", other, tag, g.iface)
			continue
		}
		if other, dup := fields[g.field]; dup {
			c.add(g.pos, "rename one tag", "tags %q and %q both become the Handlers field %s", other, tag, g.field)
			continue
		}
		ifaces[g.iface], fields[g.field] = tag, tag
	}
}

// fieldName lowers the leading capitals of a Go name, as Go spells an
// unexported name: Email gives email, APIKeys gives apiKeys. A keyword gets
// a trailing underscore, which no tag produces otherwise.
func fieldName(name string) string {
	runes := []rune(name)
	n := 0
	for n < len(runes) && unicode.IsUpper(runes[n]) {
		n++
	}
	if n > 1 && n < len(runes) && unicode.IsLower(runes[n]) {
		n-- // the last capital starts the next word: APIKeys -> api + Keys
	}
	for i := range n {
		runes[i] = unicode.ToLower(runes[i])
	}
	out := string(runes)
	if token.IsKeyword(out) {
		out += "_"
	}
	return out
}

// specOperations returns the operations of the spec in the order they are
// written. A path that is a $ref to another path of the document is an
// alias: oapi-codegen gives it no server method, so it has no handler.
func specOperations(doc *openapi3.T, spec string) []specOp {
	if doc.Paths == nil {
		return nil
	}
	var ops []specOp
	seen := map[*openapi3.Operation]bool{}
	paths := doc.Paths.Map()
	for _, path := range codegen.SortedMapKeys(paths) {
		item := paths[path]
		if item == nil || strings.HasPrefix(item.Ref, "#/paths/") {
			continue
		}
		byMethod := item.Operations()
		for _, method := range codegen.SortedMapKeys(byMethod) {
			if op := byMethod[method]; op != nil && !seen[op] {
				seen[op] = true
				ops = append(ops, newSpecOp(op, method, path, spec))
			}
		}
	}
	slices.SortStableFunc(ops, func(a, b specOp) int {
		if n := strings.Compare(a.pos.file, b.pos.file); n != 0 {
			return n
		}
		return a.pos.line - b.pos.line
	})
	return ops
}

func newSpecOp(op *openapi3.Operation, method, path, spec string) specOp {
	o := specOp{op: op, name: op.OperationID, pos: originPos(op.Origin, spec)}
	if o.name == "" {
		o.name = method + " " + path
	}
	var seq []openapi3.Location
	if op.Origin != nil {
		seq = op.Origin.Sequences["tags"]
	}
	for i := range op.Tags {
		p := o.pos
		if i < len(seq) && seq[i].Line > 0 {
			p = position{locFile(seq[i].File, spec), seq[i].Line}
		}
		o.tagPos = append(o.tagPos, p)
	}
	return o
}

// originPos returns where an element of the spec is: in its own file when a
// $ref brought it in, in the spec otherwise. The line is 0 when the loader
// recorded none.
func originPos(o *openapi3.Origin, spec string) position {
	if o == nil || o.Key == nil {
		return position{spec, 0}
	}
	return position{locFile(o.Key.File, spec), o.Key.Line}
}

// locFile returns spec, as the caller spelled it, for the spec itself, and
// file for a file a $ref brought in.
func locFile(file, spec string) string {
	if file == "" {
		return spec
	}
	absFile, errFile := filepath.Abs(file)
	absSpec, errSpec := filepath.Abs(spec)
	if errFile == nil && errSpec == nil && absFile == absSpec {
		return spec
	}
	return file
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
