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
	at        string // file:line of the declaration interface
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
	at     string // file:line of the method, for errors
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
func parseDir(dir, out string) (*loaded, *catalogue, error) {
	l, err := loadPackage(dir, out)
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
		return nil, nil, fmt.Errorf("%s: no error declarations; add an interface type whose doc comment has a "+
			"%s namespace=<name> line", dir, catalogueDirective)
	case 1:
	default:
		return nil, nil, fmt.Errorf("%s: %d %s declarations; keep one per package", dir, len(found), catalogueDirective)
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
		return nil, fmt.Errorf("%s: %s: %s must annotate an interface type", at, ts.Name.Name, catalogueDirective)
	}
	if errs := l.errorsWithin(ts); len(errs) > 0 {
		return nil, fmt.Errorf("%s: %s does not compile; fix it first:\n%s", at, ts.Name.Name, strings.Join(errs, "\n"))
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
func parseCatalogue(name string, f *ast.File, iface, args, at string) (*catalogue, error) {
	opts, err := parseArgs(args, "namespace", "type_base")
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", at, catalogueDirective, err)
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
		return nil, fmt.Errorf("%s: %s: namespace=%q must be lower snake case, for example namespace=billing",
			at, catalogueDirective, c.namespace)
	}
	if base, ok := opts["type_base"]; ok {
		u, urlErr := url.Parse(base)
		if urlErr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" ||
			u.Fragment != "" {
			return nil, fmt.Errorf("%s: %s: type_base=%q must be an absolute http(s) URL without query or fragment",
				at, catalogueDirective, base)
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
			return fmt.Errorf("%s: %s may only list methods, one per error", mpos, c.iface)
		}
		fn, _ := l.pkg.TypesInfo.Defs[m.Names[0]].(*types.Func)
		if fn == nil {
			return fmt.Errorf("%s: %s: the type checker has no signature for it", mpos, m.Names[0].Name)
		}
		d, declErr := c.parseDecl(m, ft, fn.Signature())
		if declErr != nil {
			return fmt.Errorf("%s: %s: %w", mpos, m.Names[0].Name, declErr)
		}
		d.at = mpos
		if prev, dup := seenCode[d.code]; dup {
			return fmt.Errorf("%s: %s: code %q is already used by %s; set a distinct code=", mpos, d.name, d.code, prev)
		}
		seenCode[d.code] = d.name
		c.errors = append(c.errors, d)
	}
	if len(c.errors) == 0 {
		return fmt.Errorf("%s: %s declares no errors; add a method per error", c.at, c.iface)
	}
	return nil
}

func (c *catalogue) parseDecl(m *ast.Field, ft *ast.FuncType, sig *types.Signature) (decl, error) {
	d := decl{name: m.Names[0].Name, doc: strings.TrimSpace(m.Doc.Text())}
	if !ast.IsExported(d.name) {
		return d, fmt.Errorf("error names must be exported; rename to %s", exportName(d.name))
	}
	args, annotated := directive(m.Doc, errorDirective)
	if !annotated {
		return d, fmt.Errorf("missing %s line; add %s code=%s status=<4xx|5xx> title=\"...\" to its doc comment",
			errorDirective, errorDirective, snakeName(d.name))
	}
	opts, err := parseArgs(args, "code", "status", "title", "detail")
	if err != nil {
		return d, fmt.Errorf("%s: %w", errorDirective, err)
	}
	if err = d.applyOptions(opts); err != nil {
		return d, err
	}
	if ft.Results != nil && len(ft.Results.List) > 0 {
		return d, errors.New("declarations take parameters only; remove the results")
	}
	if d.params, err = c.parseParams(d, sig); err != nil {
		return d, err
	}
	return d, d.checkDetail()
}

// applyOptions validates and stores the //nano:error options.
func (d *decl) applyOptions(opts map[string]string) error {
	status, err := strconv.Atoi(opts["status"])
	if err != nil || status < 400 || status > 599 || http.StatusText(status) == "" {
		return fmt.Errorf("status=%q must be a known HTTP error status (400-599)", opts["status"])
	}
	d.status = status
	d.title = opts["title"]
	switch {
	case strings.TrimSpace(d.title) == "":
		return errors.New(`title is required; add title="..." (a short summary that never changes per occurrence)`)
	case strings.Contains(d.title, "{"):
		return errors.New("title must not hold placeholders; put occurrence data in detail=")
	}
	code, set := opts["code"]
	switch {
	case !set:
		// The code and the type URI are the client contract. Deriving them
		// from the Go name would let a rename for readability change them.
		return fmt.Errorf("code is required; add code=%s (the code and the problem type URI are the public "+
			"contract, so they never follow a rename of the Go method)", snakeName(d.name))
	case !identPattern.MatchString(code):
		return fmt.Errorf("code=%q must be lower snake case, for example code=%s", code, snakeName(d.name))
	}
	d.code = code
	d.detail = opts["detail"]
	return nil
}

// parseParams reads the parameters from the type-checked signature.
func (c *catalogue) parseParams(d decl, sig *types.Signature) ([]param, error) {
	if sig.Variadic() {
		return nil, errors.New("variadic parameters are not supported")
	}
	typeName := d.name + "Error"
	fields := map[string]string{}
	params := make([]param, 0, sig.Params().Len())
	for v := range sig.Params().Variables() {
		name := v.Name()
		if name == "" {
			return nil, errors.New("every parameter needs a name; it becomes a field of the error")
		}
		field := exportName(name)
		switch {
		case name == "_" || slices.Contains(reserved, field):
			return nil, fmt.Errorf("parameter %q clashes with a generated name; rename it", name)
		case name == typeName:
			return nil, fmt.Errorf("parameter %q shadows the generated type %s; rename it", name, typeName)
		}
		if prev, dup := fields[field]; dup {
			return nil, fmt.Errorf("parameters %q and %q both become field %s; rename one", prev, name, field)
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
				return fmt.Errorf("%s: %s: generated name %s clashes with %s; rename the method", d.at, d.name, n, prev)
			}
			owner[n] = "the generated code of " + d.name
		}
	}
	for _, d := range c.errors {
		for _, n := range []string{d.name, d.name + "Error", "Err" + d.name} {
			if obj, at := l.lookup(n); obj != nil {
				return fmt.Errorf("%s: %s: generated name %s clashes with %s declared at %s; rename one",
					d.at, d.name, n, obj.Name(), at)
			}
		}
	}
	if obj, at := l.lookup(kindsVar); obj != nil {
		return fmt.Errorf("%s: %s: the generated %s list clashes with %s declared at %s; rename it",
			c.at, c.iface, kindsVar, obj.Name(), at)
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
func (d *decl) checkDetail() error {
	for _, m := range placeholderPattern.FindAllStringSubmatch(d.detail, -1) {
		if !slices.ContainsFunc(d.params, func(p param) bool { return p.name == m[1] }) {
			return fmt.Errorf("detail placeholder {%s} names no parameter; use one of the parameter names", m[1])
		}
	}
	if rest := placeholderPattern.ReplaceAllString(d.detail, ""); strings.ContainsAny(rest, "{}") {
		return errors.New("detail has an unmatched brace; placeholders look like {name}")
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

// directive returns the arguments of the first doc-comment line that starts
// with prefix.
func directive(doc *ast.CommentGroup, prefix string) (string, bool) {
	if doc == nil {
		return "", false
	}
	for _, c := range doc.List {
		if rest, ok := strings.CutPrefix(c.Text, prefix); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// parseArgs reads key=value pairs. A value is either bare (no spaces) or a Go
// double-quoted string. Keys outside allowed are rejected.
func parseArgs(s string, allowed ...string) (map[string]string, error) {
	out := map[string]string{}
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		eq := strings.IndexByte(s, '=')
		if eq <= 0 || strings.ContainsAny(s[:eq], " \t\"") {
			return nil, fmt.Errorf("expected key=value at %q", s)
		}
		key := s[:eq]
		val, rest, err := scanValue(key, s[eq+1:])
		if err != nil {
			return nil, err
		}
		s = rest
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("unknown option %q; valid options: %s", key, strings.Join(allowed, ", "))
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("option %s is set twice", key)
		}
		out[key] = val
	}
	return out, nil
}

// scanValue reads the value at the start of s, quoted or bare, and returns it
// with the rest of s.
func scanValue(key, s string) (string, string, error) {
	if !strings.HasPrefix(s, `"`) {
		end := strings.IndexAny(s, " \t")
		if end < 0 {
			end = len(s)
		}
		return s[:end], s[end:], nil
	}
	end := quotedEnd(s)
	if end < 0 {
		return "", "", fmt.Errorf("unterminated quoted value for %s", key)
	}
	v, err := strconv.Unquote(s[:end])
	if err != nil {
		return "", "", fmt.Errorf("bad quoted value for %s: %w", key, err)
	}
	return v, s[end:], nil
}

// quotedEnd returns the index just past the closing quote of the Go string
// literal at the start of s, or -1.
func quotedEnd(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}
