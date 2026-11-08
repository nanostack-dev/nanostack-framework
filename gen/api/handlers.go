package genapi

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// HandlersFile is the file of the tag split, written next to the server.
const HandlersFile = "handlers.gen.go"

// The names handlers.gen.go declares besides one interface per tag.
const (
	handlersType = "Handlers"
	handlersFunc = "NewHandlers"
	// checkMethod is the unexported method of Handlers that refuses a nil
	// handler. fieldName keeps every field off its name.
	checkMethod = "checked"
)

// renderHandlers writes handlers.gen.go: one interface per tag, the
// Handlers type that implements StrictServerInterface by passing each
// request to the handler of the operation's tag, and NewHandlers. defs are
// oapi-codegen's operations, read with the server's options set; server is
// the server's code, whose top-level names the file must not declare again.
func renderHandlers(pkg, header string, groups []*tagGroup, defs []codegen.OperationDefinition,
	server, spec string,
) ([]byte, error) {
	if err := clashes(groups, server, spec); err != nil {
		return nil, err
	}
	if err := assign(groups, defs, spec); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(header)
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	if len(groups) > 0 {
		b.WriteString("import (\n\t\"context\"\n\t\"errors\"\n)\n\n")
	}
	for _, g := range groups {
		writeInterface(&b, g)
	}
	writeHandlers(&b, groups)
	for _, g := range groups {
		for _, op := range g.ops {
			fmt.Fprintf(&b, "// %s passes the request to the %s handler.\n", op.OperationId, quoteTag(g.tag))
			fmt.Fprintf(&b, "func (h *Handlers) %s {\nreturn h.%s.%s(ctx, request)\n}\n\n",
				signature(op), g.field, op.OperationId)
		}
	}
	b.WriteString("var _ StrictServerInterface = (*Handlers)(nil)\n")

	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, gen.Errorf(spec, 0, "report it: nano gen api wrote a "+HandlersFile+" that does not parse",
			"format %s: %v", HandlersFile, err)
	}
	return src, nil
}

// assign gives each group its operations, in oapi-codegen's order. An
// alias has no server method.
func assign(groups []*tagGroup, defs []codegen.OperationDefinition, spec string) error {
	byTag := make(map[string]*tagGroup, len(groups))
	for _, g := range groups {
		g.ops = nil
		byTag[g.tag] = g
	}
	for _, op := range defs {
		if op.IsAlias {
			continue
		}
		var g *tagGroup
		if op.Spec != nil && len(op.Spec.Tags) == 1 {
			g = byTag[op.Spec.Tags[0]]
		}
		if g == nil {
			return gen.Errorf(spec, 0, "report it: the tag rules passed an operation without one tag",
				"operation %s has no tag group", op.OperationId)
		}
		g.ops = append(g.ops, op)
	}
	return nil
}

// writeInterface writes the interface of one tag, with the comments
// oapi-codegen's strict-interface template gives each method.
func writeInterface(b *bytes.Buffer, g *tagGroup) {
	fmt.Fprintf(b, "// %s serves the operations tagged %s.\n", g.iface, quoteTag(g.tag))
	fmt.Fprintf(b, "type %s interface {\n", g.iface)
	for i, op := range g.ops {
		if i > 0 {
			b.WriteString("\n")
		}
		if c := op.SummaryAsComment(op.OperationId); c != "" {
			b.WriteString(c + "\n")
		}
		fmt.Fprintf(b, "// (%s %s)\n", op.Method, op.Path)
		if d := op.DeprecationComment(); d != "" {
			b.WriteString("//\n" + d + "\n")
		}
		b.WriteString(signature(op) + "\n")
	}
	b.WriteString("}\n\n")
}

// writeHandlers writes the Handlers type, NewHandlers and its nil check.
// The check is a method: inside NewHandlers, a parameter named after a tag
// could shadow a name the check uses.
func writeHandlers(b *bytes.Buffer, groups []*tagGroup) {
	b.WriteString("// Handlers implements StrictServerInterface: each method passes the request\n" +
		"// to the handler of the operation's tag. NewHandlers builds it.\n")
	b.WriteString("type Handlers struct {\n")
	for _, g := range groups {
		fmt.Fprintf(b, "%s %s\n", g.field, g.iface)
	}
	b.WriteString("}\n\n")
	b.WriteString("// NewHandlers returns the Handlers that serve each tag with its handler. It\n" +
		"// returns an error that names the tag of a nil handler.\n")
	if len(groups) == 0 {
		b.WriteString("func NewHandlers() (*Handlers, error) {\nreturn &Handlers{}, nil\n}\n\n")
		return
	}
	b.WriteString("func NewHandlers(\n")
	for _, g := range groups {
		fmt.Fprintf(b, "%s %s,\n", g.field, g.iface)
	}
	b.WriteString(") (*Handlers, error) {\nreturn (&Handlers{\n")
	for _, g := range groups {
		fmt.Fprintf(b, "%s: %s,\n", g.field, g.field)
	}
	fmt.Fprintf(b, "}).%s()\n}\n\n", checkMethod)
	fmt.Fprintf(b, "// %s returns h, or an error when a handler is nil.\n", checkMethod)
	fmt.Fprintf(b, "func (h *Handlers) %s() (*Handlers, error) {\nswitch {\n", checkMethod)
	for _, g := range groups {
		fmt.Fprintf(b, "case h.%s == nil:\nreturn nil, errors.New(%s)\n", g.field,
			strconv.Quote("NewHandlers: the handler of tag "+quoteTag(g.tag)+" is nil"))
	}
	b.WriteString("}\nreturn h, nil\n}\n\n")
}

// signature is the method of an operation in StrictServerInterface, as
// oapi-codegen's strict-interface template writes it.
func signature(op codegen.OperationDefinition) string {
	id := op.OperationId
	typ := ucFirst(id)
	return fmt.Sprintf("%s(ctx context.Context, request %sRequestObject) (%sResponseObject, error)", id, typ, typ)
}

func ucFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// quoteTag returns a tag for a comment or a message: as written when it is
// one word, quoted otherwise.
func quoteTag(tag string) string {
	if strings.ContainsFunc(tag, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		return strconv.Quote(tag)
	}
	return tag
}

// clashes reports a name of handlers.gen.go that the server already
// declares, such as a schema named EmailServer or Handlers.
func clashes(groups []*tagGroup, server, spec string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "", server, parser.SkipObjectResolution)
	if err != nil {
		return gen.Errorf(spec, 0, "report it: oapi-codegen wrote a server that does not parse",
			"parse the server: %v", err)
	}
	declared := topLevelNames(f)
	var errs []error
	for _, name := range []string{handlersType, handlersFunc} {
		if declared[name] {
			errs = append(errs, gen.Errorf(spec, 0,
				"rename the schema or operation that oapi-codegen turns into "+name+", for example with x-go-name",
				"the server declares %s, a name that %s declares", name, HandlersFile))
		}
	}
	for _, g := range groups {
		if declared[g.iface] {
			errs = append(errs, gen.Errorf(g.pos.file, g.pos.line,
				"rename the tag, or the schema or operation that oapi-codegen turns into "+g.iface,
				"tag %s becomes %s, which the server already declares", g.tag, g.iface))
		}
	}
	return errors.Join(errs...)
}

// topLevelNames returns the package-level names f declares.
func topLevelNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				names[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					names[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names[n.Name] = true
					}
				}
			}
		}
	}
	return names
}
