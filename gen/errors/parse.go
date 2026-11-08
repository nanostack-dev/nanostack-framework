package generrors

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/gen"
)

const (
	catalogueDirective = "//nano:errors"
	errorDirective     = "//nano:error"
)

var (
	identPattern       = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	placeholderPattern = regexp.MustCompile(`\{([^{}]*)\}`)
	// reserved are generated method and field names a parameter must not take.
	reserved = []string{
		"Error",
		"Unwrap",
		"Wrap",
		"Is",
		"ProblemType",
		"ProblemStatus",
		"ProblemTitle",
		"ProblemCode",
		"ProblemDetail",
	}
)

// kindsVar is the generated list of every Kind.
const kindsVar = "Kinds"

// catalogue is one parsed declaration file.
type catalogue struct {
	pkg       string
	pkgPath   string
	file      string // file name relative to the package directory
	at        pos    // where the declaration interface is
	iface     string
	namespace string
	typeBase  string
	errors    []decl
	// aliases maps import path to the name the declaration file gives it, so
	// generated code and docs keep the names the author chose.
	aliases map[string]string
}

type decl struct {
	name   string // Go name of the constructor, e.g. InvoiceAlreadyPaid
	code   string // name part of the code, e.g. invoice_already_paid
	status int
	title  string
	detail string // template with {param} placeholders; may be empty
	doc    string
	at     pos // where the method is, for errors
	params []param
}

type param struct {
	name   string     // parameter name, e.g. paidAt
	field  string     // exported struct field, e.g. PaidAt
	typ    types.Type // resolved by the type checker
	docTyp string     // type as the declaration file names it, e.g. time.Time
	isTime bool       // time.Time formats as RFC 3339 in detail and Error
}

// parseDir loads and type-checks the package in dir and finds its one
// declaration interface. Types come from the type checker, so every package
// is named as Go resolves it, whatever its import path looks like.
func parseDir(opts Options) (*loaded, *catalogue, error) {
	dir := opts.Dir
	l, err := loadPackage(opts)
	if err != nil {
		return nil, nil, err
	}
	files, names := l.syntax()
	var found []*catalogue
	for i, f := range files {
		cats, fileErr := parseFile(l, names[i], f)
		if fileErr != nil {
			return nil, nil, fileErr
		}
		found = append(found, cats...)
	}
	switch len(found) {
	case 0:
		return nil, nil, gen.Errorf(dir, 0,
			"add an interface type whose doc comment has a "+catalogueDirective+" namespace=<name> line",
			"no error declarations")
	case 1:
	default:
		return nil, nil, gen.Errorf(dir, 0, "keep one per package; move the others to packages of their own",
			"%d %s declarations", len(found), catalogueDirective)
	}
	c := found[0]
	if err = c.checkNames(l); err != nil {
		return nil, nil, err
	}
	return l, c, nil
}

func parseFile(l *loaded, name string, f *ast.File) ([]*catalogue, error) {
	var out []*catalogue
	for _, d := range f.Decls {
		gd, isGen := d.(*ast.GenDecl)
		if !isGen || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, _ := spec.(*ast.TypeSpec)
			doc := ts.Doc
			if doc == nil && len(gd.Specs) == 1 {
				doc = gd.Doc
			}
			c, err := parseSpec(l, name, f, ts, doc)
			if err != nil {
				return nil, err
			}
			if c != nil {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// parseSpec returns the catalogue declared by ts, or nil when ts carries no
// //nano:errors directive.
func parseSpec(l *loaded, name string, f *ast.File, ts *ast.TypeSpec, doc *ast.CommentGroup) (*catalogue, error) {
	args, annotated := directive(doc, catalogueDirective)
	if !annotated {
		return nil, nil //nolint:nilnil // not annotated is a normal outcome, not a failure
	}
	at := l.position(ts.Pos())
	it, isIface := ts.Type.(*ast.InterfaceType)
	if !isIface {
		return nil, at.errorf(
			"declare "+ts.Name.Name+" as an interface with one method per error, or remove the directive",
			"%s: %s must annotate an interface type",
			ts.Name.Name,
			catalogueDirective,
		)
	}
	if errs := l.errorsWithin(ts); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	c, err := parseCatalogue(name, f, ts.Name.Name, args, at)
	if err != nil {
		return nil, err
	}
	c.pkgPath = l.pkg.PkgPath
	if err = c.parseMethods(l, it); err != nil {
		return nil, err
	}
	return c, nil
}

// parseCatalogue reads the //nano:errors options.
func parseCatalogue(name string, f *ast.File, iface, args string, at pos) (*catalogue, error) {
	opts, err := parseArgs(args, "namespace", "type_base")
	if err != nil {
		return nil, at.wrap(catalogueDirective, err)
	}
	c := &catalogue{
		pkg:       f.Name.Name,
		file:      name,
		at:        at,
		iface:     iface,
		namespace: opts["namespace"],
		typeBase:  fault.DefaultTypeBase,
		aliases:   importAliases(f),
	}
	if !identPattern.MatchString(c.namespace) {
		return nil, at.errorf("write it in lower snake case, for example namespace=billing",
			"%s: namespace=%q must be lower snake case", catalogueDirective, c.namespace)
	}
	if base, ok := opts["type_base"]; ok {
		u, urlErr := url.Parse(base)
		if urlErr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" ||
			u.Fragment != "" {
			return nil, at.errorf("set it to a URL such as https://errors.example.com, or remove it for the default",
				"%s: type_base=%q must be an absolute http(s) URL without query or fragment", catalogueDirective, base)
		}
		c.typeBase = strings.TrimSuffix(base, "/")
	}
	return c, nil
}

// parseMethods turns each interface method into an error declaration.
func (c *catalogue) parseMethods(l *loaded, it *ast.InterfaceType) error {
	seenCode := map[string]string{}
	for _, m := range it.Methods.List {
		mpos := l.position(m.Pos())
		ft, isFunc := m.Type.(*ast.FuncType)
		if !isFunc || len(m.Names) != 1 {
			return mpos.errorf("declare each error as a method of its own; move embedded interfaces out",
				"%s may only list methods, one per error", c.iface)
		}
		fn, _ := l.pkg.TypesInfo.Defs[m.Names[0]].(*types.Func)
		if fn == nil {
			return mpos.errorf("fix the method so that it type-checks",
				"%s: the type checker has no signature for it", m.Names[0].Name)
		}
		d, declErr := c.parseDecl(m, ft, fn.Signature())
		if declErr != nil {
			return mpos.wrap(m.Names[0].Name, declErr)
		}
		d.at = mpos
		if prev, dup := seenCode[d.code]; dup {
			return mpos.errorf("set a distinct code=", "%s: code %q is already used by %s", d.name, d.code, prev)
		}
		seenCode[d.code] = d.name
		c.errors = append(c.errors, d)
	}
	if len(c.errors) == 0 {
		return c.at.errorf("add a method per error", "%s declares no errors", c.iface)
	}
	return nil
}

func (c *catalogue) parseDecl(m *ast.Field, ft *ast.FuncType, sig *types.Signature) (decl, *gen.Error) {
	d := decl{name: m.Names[0].Name, doc: strings.TrimSpace(m.Doc.Text())}
	if !ast.IsExported(d.name) {
		return d, problem("rename it to "+exportName(d.name), "error names must be exported")
	}
	args, annotated := directive(m.Doc, errorDirective)
	if !annotated {
		return d, problem(fmt.Sprintf("add %s code=%s status=<4xx|5xx> title=\"...\" to its doc comment",
			errorDirective, snakeName(d.name)), "missing %s line", errorDirective)
	}
	opts, err := parseArgs(args, "code", "status", "title", "detail")
	if err != nil {
		return d, problem(err.Fix, "%s: %s", errorDirective, err.Msg)
	}
	if err = d.applyOptions(opts); err != nil {
		return d, err
	}
	if ft.Results != nil && len(ft.Results.List) > 0 {
		return d, problem("remove the results", "declarations take parameters only")
	}
	if d.params, err = c.parseParams(d, sig); err != nil {
		return d, err
	}
	return d, d.checkDetail()
}

// applyOptions validates and stores the //nano:error options.
func (d *decl) applyOptions(opts map[string]string) *gen.Error {
	status, err := strconv.Atoi(opts["status"])
	if err != nil || status < 400 || status > 599 || http.StatusText(status) == "" {
		return problem("set status= to a 4xx or 5xx code, for example status=404",
			"status=%q must be a known HTTP error status (400-599)", opts["status"])
	}
	d.status = status
	d.title = opts["title"]
	switch {
	case strings.TrimSpace(d.title) == "":
		return problem(`add title="..." (a short summary that never changes per occurrence)`, "title is required")
	case strings.Contains(d.title, "{"):
		return problem("move occurrence data to detail=", "title must not hold placeholders")
	}
	code, set := opts["code"]
	switch {
	case !set:
		// The code and the type URI are the client contract. Deriving them
		// from the Go name would let a rename for readability change them.
		return problem(fmt.Sprintf("add code=%s; the code and the problem type URI are the public contract, "+
			"so they never follow a rename of the Go method", snakeName(d.name)), "code is required")
	case !identPattern.MatchString(code):
		return problem("write it in lower snake case, for example code="+snakeName(d.name),
			"code=%q must be lower snake case", code)
	}
	d.code = code
	d.detail = opts["detail"]
	return nil
}

// parseParams reads the parameters from the type-checked signature.
func (c *catalogue) parseParams(d decl, sig *types.Signature) ([]param, *gen.Error) {
	if sig.Variadic() {
		return nil, problem("take a slice instead", "variadic parameters are not supported")
	}
	typeName := d.name + "Error"
	fields := map[string]string{}
	params := make([]param, 0, sig.Params().Len())
	for v := range sig.Params().Variables() {
		name := v.Name()
		if name == "" {
			return nil, problem("name each parameter; it becomes a field of the error", "a parameter has no name")
		}
		field := exportName(name)
		switch {
		case name == "_" || slices.Contains(reserved, field):
			return nil, problem("rename it", "parameter %q clashes with a generated name", name)
		case name == typeName:
			return nil, problem("rename it", "parameter %q shadows the generated type %s", name, typeName)
		}
		if prev, dup := fields[field]; dup {
			return nil, problem("rename one", "parameters %q and %q both become field %s", prev, name, field)
		}
		fields[field] = name
		params = append(params, param{
			name:   name,
			field:  field,
			typ:    v.Type(),
			docTyp: types.TypeString(v.Type(), c.docQualifier),
			isTime: isTimeType(v.Type()),
		})
	}
	return params, nil
}

// checkNames proves that no generated package-level name clashes with
// another one or with a name the package declares by hand. The type check in
// verify would catch a clash too, but only with a position in the generated
// file; this names the declaration to rename.
func (c *catalogue) checkNames(l *loaded) error {
	owner := map[string]string{kindsVar: "the generated " + kindsVar + " list"}
	for _, d := range c.errors {
		for _, n := range []string{d.name, d.name + "Error", "Err" + d.name} {
			if prev, dup := owner[n]; dup {
				return d.at.errorf("rename the method", "%s: generated name %s clashes with %s", d.name, n, prev)
			}
			owner[n] = "the generated code of " + d.name
		}
	}
	for _, d := range c.errors {
		for _, n := range []string{d.name, d.name + "Error", "Err" + d.name} {
			if obj, at := l.lookup(n); obj != nil {
				return d.at.errorf("rename one of them", "%s: generated name %s clashes with %s declared at %s",
					d.name, n, obj.Name(), at)
			}
		}
	}
	if obj, at := l.lookup(kindsVar); obj != nil {
		return c.at.errorf("rename "+obj.Name(), "%s: the generated %s list clashes with %s declared at %s",
			c.iface, kindsVar, obj.Name(), at)
	}
	return nil
}

// docQualifier names packages as the declaration file does, for the
// catalogue and error messages.
func (c *catalogue) docQualifier(p *types.Package) string {
	if p.Path() == c.pkgPath {
		return ""
	}
	if alias, ok := c.aliases[p.Path()]; ok {
		return alias
	}
	return p.Name()
}

func isTimeType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Time"
}

// checkDetail proves every {placeholder} names a parameter.
func (d *decl) checkDetail() *gen.Error {
	for _, m := range placeholderPattern.FindAllStringSubmatch(d.detail, -1) {
		if !slices.ContainsFunc(d.params, func(p param) bool { return p.name == m[1] }) {
			names := make([]string, len(d.params))
			for i, p := range d.params {
				names[i] = "{" + p.name + "}"
			}
			fix := "add a parameter of that name, or remove the placeholder"
			if len(names) > 0 {
				fix = "use one of " + strings.Join(names, ", ") + ", or add a parameter of that name"
			}
			return problem(fix, "detail placeholder {%s} names no parameter", m[1])
		}
	}
	if rest := placeholderPattern.ReplaceAllString(d.detail, ""); strings.ContainsAny(rest, "{}") {
		return problem("write placeholders as {name}, with no other brace in detail=", "detail has an unmatched brace")
	}
	return nil
}

// importAliases maps import path to the explicit name the file gives it.
// Unnamed imports are left out: the type checker knows their real names.
func importAliases(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		if imp.Name == nil || imp.Name.Name == "_" || imp.Name.Name == "." {
			continue
		}
		path, _ := strconv.Unquote(imp.Path.Value)
		out[path] = imp.Name.Name
	}
	return out
}
