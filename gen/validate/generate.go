// Package genvalidate implements nanostack gen validate: it reads an OpenAPI 3.0 or
// 3.1 document and the package oapi-codegen generated from it, and writes a
// Validate(validate.Path) validate.Errors method for every request body
// type, plus a validateParams method for every operation with parameters.
//
// The generated code is plain Go over the oapi-codegen types and the
// core/validate runtime. Nothing reads the spec at runtime.
//
// The Go package must contain the models and the strict server
// (StrictServerInterface) in one package: operations are joined to the spec
// by the "(METHOD /path)" comment oapi-codegen writes on each interface
// method, so oapi-codegen's naming rules are never re-derived.
package genvalidate

import (
	"errors"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/oas"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
)

// Options configures one run: the inputs, the output and what Run accepts.
type Options struct {
	// Spec is the OpenAPI document.
	Spec string
	// Dir is the directory of the Go package oapi-codegen generated.
	Dir string
	// Output is the generated file name inside Dir. Default validate.gen.go.
	Output string
	// Check makes Run compare instead of writing.
	Check bool
	// Overlay replaces Go files in the package load, keyed by absolute
	// path, as in golang.org/x/tools/go/packages. The output file is always
	// replaced by an empty one. The spec is an input and is read from disk.
	Overlay map[string][]byte
}

// DefaultOutput is the generated file name when Options.Output is empty.
const DefaultOutput = "validate.gen.go"

// Diagnostic is a schema feature the generated code does not enforce.
type Diagnostic struct {
	// Loc is the JSON Pointer of the schema in the spec.
	Loc     string
	Message string
	// Line is the line of the schema in the spec, or 0 when it is unknown.
	Line int
	// Waived is the reason of the x-nano-unenforced extension that accepts
	// the gap, on the schema or on one it is nested in. Empty when no
	// extension accepts it.
	Waived string
}

// String is the location and the message. Spec text in them is escaped, so
// it cannot end a comment in the generated file or drive a terminal.
func (d Diagnostic) String() string { return commentText(d.Loc + ": " + d.Message) }

// Result is the generated file and what it could not enforce. It is the one
// variation on the gen contract: Generate returns the file in a Result, not
// a []gen.File, because the diagnostics come with it.
type Result struct {
	File        gen.File
	Diagnostics []Diagnostic
	unused      []oas.Waiver // the x-nano-unenforced extensions that accept no gap
	spec        string       // the spec path, as Options.Spec gave it
}

// Gaps returns the problems of the spec that do not stop the file from being
// written, joined: one *gen.Error wrapping ErrUnenforced per diagnostic that
// no x-nano-unenforced extension accepts, and one per extension that accepts
// nothing, which would hide the next gap. It returns nil when there are none.
func (r *Result) Gaps() error {
	var errs []error
	for _, d := range r.Diagnostics {
		if d.Waived != "" {
			continue
		}
		errs = append(errs, &gen.Error{
			File: r.spec,
			Line: d.Line,
			Msg:  "not enforced: " + d.String(),
			Fix: "change the schema, or accept the gap with " + oas.WaiverKey + `: "<reason>" on the schema at ` +
				commentText(d.Loc),
			Err: ErrUnenforced,
		})
	}
	for _, w := range r.unused {
		errs = append(errs, &gen.Error{
			File: r.spec,
			Line: w.Line,
			Msg:  commentText(w.Loc) + ": " + oas.WaiverKey + " accepts no gap",
			Fix:  "delete the " + oas.WaiverKey + " extension",
			Err:  ErrUnenforced,
		})
	}
	return errors.Join(errs...)
}

// Generate produces the validators. It fails when the spec and the Go types
// disagree, or when a pattern cannot be expressed in RE2; schema features
// the generator does not support are returned as diagnostics.
func Generate(cfg Options) (*Result, error) {
	if cfg.Output == "" {
		cfg.Output = DefaultOutput
	}
	gp, err := loadPackage(cfg)
	if err != nil {
		return nil, err
	}
	return generateWith(cfg, gp)
}

// generateWith generates against an already loaded package.
func generateWith(cfg Options, gp *goPackage) (*Result, error) {
	if cfg.Output == "" {
		cfg.Output = DefaultOutput
	}
	spec, err := oas.LoadSpec(cfg.Spec)
	if err != nil {
		return nil, err
	}
	g := newGenerator(spec, gp)
	src, err := g.run(relativeTo(cfg.Dir, cfg.Spec))
	if err != nil {
		return nil, err
	}
	diags := g.diagnostics()
	for i := range diags {
		diags[i].Line = spec.Line(diags[i].Loc)
		diags[i].Waived, _ = spec.Waive(diags[i].Loc)
	}
	return &Result{
		File:        gen.File{Path: filepath.Join(cfg.Dir, cfg.Output), Data: src},
		Diagnostics: diags,
		unused:      spec.Unused(),
		spec:        cfg.Spec,
	}, nil
}

// relativeTo returns path relative to dir, slash-separated, so the header
// of the generated file is the same on every machine.
func relativeTo(dir, path string) string {
	absPath, errPath := filepath.Abs(path)
	absDir, errDir := filepath.Abs(dir)
	if errPath != nil || errDir != nil {
		return path
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

type method struct {
	named  *types.Named
	schema *oas.Schema
	body   string
}

type paramsMethod struct {
	req  *types.Named
	op   *oas.Operation
	body string
}

type generator struct {
	spec *oas.Spec
	gp   *goPackage

	methods  map[*types.Named]*method
	queue    []*method
	params   []*paramsMethod
	patterns map[string]string
	patOrder []string
	imports  *importset.Set
	renamed  map[string]bool // fixed imports already reported as taken
	diags    map[Diagnostic]bool
	errs     []error
	n        int
}

func newGenerator(spec *oas.Spec, gp *goPackage) *generator {
	return &generator{
		spec: spec, gp: gp,
		methods:  map[*types.Named]*method{},
		patterns: map[string]string{},
		imports:  newImports(gp.pkg),
		renamed:  map[string]bool{},
		diags:    map[Diagnostic]bool{},
	}
}

func (g *generator) run(specRel string) ([]byte, error) {
	ops := map[string]*oas.Operation{}
	for _, op := range g.spec.Operations {
		ops[op.Method+" "+op.Path] = op
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range g.gp.ops {
			if !yield(k) {
				return
			}
		}
	})
	for _, key := range keys {
		gop := g.gp.ops[key]
		op, ok := ops[key]
		if !ok {
			g.goErrorf(gop.Pos, regenerateFix, "StrictServerInterface.%s: no operation %s in the spec", gop.Name, key)
			continue
		}
		g.operation(gop, op)
	}
	for _, pm := range g.params {
		pm.body = g.paramsBody(pm)
	}
	for len(g.queue) > 0 {
		m := g.queue[0]
		g.queue = g.queue[1:]
		m.body = g.methodBody(m)
	}
	if len(g.errs) > 0 {
		return nil, oas.JoinErrors(g.errs)
	}
	return g.render(specRel)
}

// methodBody is the body of the Validate method of m. It starts with the
// value at p itself: a JSON null decodes into a Go struct, slice or scalar
// without an error, so a schema that does not accept null rejects it here.
// Every caller inside generated code has already checked null at its own
// site, so this check only fires for the value nanohttp.Decode hands over,
// the request body, which has no parent to check it. A null that the schema
// accepts ends the checks, which are for a value.
func (g *generator) methodBody(m *method) string {
	checks := g.value(m.schema, "r", m.named, "p", ctx{})
	if !g.isNullable(m.schema) {
		return "if !errs.NotNull(p) {\nreturn\n}\n" + checks
	}
	if checks == "" {
		return ""
	}
	return "if p.IsNull() {\nreturn\n}\n" + checks
}

// operation joins one strict-server operation to the spec.
func (g *generator) operation(gop *goOperation, op *oas.Operation) {
	st, ok := structOf(gop.Request)
	if !ok {
		g.goErrorf(gop.Pos, regenerateFix, "%s: %s is not a struct", op.Loc, gop.Request.Obj().Name())
		return
	}
	bodies, hasParams := requestFields(st)
	content := jsonContent(op)
	for _, f := range bodies {
		// A text or form body also ends in RequestBody; only JSON ones go
		// through Decode.
		if mt, matched := g.matchBody(f, content); matched {
			g.body(op, f, mt)
		}
	}
	if hasParams {
		g.params = append(g.params, &paramsMethod{req: gop.Request, op: op})
	}
}

// requestFields splits a RequestObject's fields into body fields and the
// parameters, which are every other field but ContentType.
func requestFields(st *types.Struct) (bodies []*types.Var, hasParams bool) {
	for f := range st.Fields() {
		switch {
		case strings.HasSuffix(f.Name(), "Body"):
			if isRequestBodyPointer(f.Type()) {
				bodies = append(bodies, f)
			}
		case f.Name() == "ContentType":
		default:
			hasParams = true
		}
	}
	return bodies, hasParams
}

func isRequestBodyPointer(t types.Type) bool {
	ptr, isPtr := t.(*types.Pointer)
	if !isPtr {
		return false
	}
	n, named := ptr.Elem().(interface{ Obj() *types.TypeName })
	return named && strings.HasSuffix(n.Obj().Name(), "RequestBody")
}

func jsonContent(op *oas.Operation) []oas.MediaType {
	if op.Body == nil {
		return nil
	}
	var out []oas.MediaType
	for _, mt := range op.Body.Content {
		if isJSONMediaType(mt.Type) {
			out = append(out, mt)
		}
	}
	return out
}

// body assigns the request body schema to the body's Go type, whose
// Validate method the strict server calls through nanohttp.Decode.
func (g *generator) body(op *oas.Operation, f *types.Var, mt oas.MediaType) {
	ptr, _ := f.Type().(*types.Pointer) // checked by isRequestBodyPointer
	elem := types.Unalias(ptr.Elem())
	n := g.localNamed(elem)
	if n == nil {
		g.errorf("give the body schema a component without x-go-type",
			"%s: request body type %s cannot have a Validate method (it is not a type defined in this package)",
			op.Loc, types.TypeString(elem, nil))
		return
	}
	sc := mt.Schema
	if sc == nil {
		sc = &oas.Schema{Loc: op.Loc + "/requestBody"}
	}
	if !g.claim(n, g.spec.Resolve(sc)) {
		g.errorf("give each request body schema a Go type of its own",
			"%s: Go type %s already validates another schema", op.Loc, n.Obj().Name())
	}
	if sc.Siblings() != nil {
		g.diag(sc.Loc, "constraints next to the request body $ref are not enforced")
	}
}

// matchBody pairs a Body field with its JSON media type. oapi-codegen names
// the field Body for a single body and <Tag>Body when there are several; the
// tag of application/json is JSON.
func (g *generator) matchBody(f *types.Var, content []oas.MediaType) (oas.MediaType, bool) {
	if f.Name() == "Body" {
		if len(content) == 1 {
			return content[0], true
		}
		return oas.MediaType{}, false
	}
	tag := strings.TrimSuffix(f.Name(), "Body")
	for _, mt := range content {
		if (mt.Type == "application/json" && tag == "JSON") || mediaTypeTag(mt.Type) == tag {
			return mt, true
		}
	}
	return oas.MediaType{}, false
}

// mediaTypeTag approximates oapi-codegen's tag for a +json media type.
func mediaTypeTag(mt string) string {
	var b strings.Builder
	up := true
	for _, r := range mt {
		switch {
		case r == '+':
			b.WriteString("Plus")
			up = true
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			if up {
				b.WriteString(strings.ToUpper(string(r)))
			} else {
				b.WriteRune(r)
			}
			up = false
		default:
			up = true
		}
	}
	return strings.ReplaceAll(b.String(), "Json", "JSON")
}

func isJSONMediaType(mt string) bool {
	mt, _, _ = strings.Cut(mt, ";")
	mt = strings.TrimSpace(strings.ToLower(mt))
	return mt == "application/json" || strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json")
}

// claim assigns schema sc to the in-package named type n, whose Validate
// method will check it. It reports false when n already checks another
// schema; the caller then inlines the checks.
func (g *generator) claim(n *types.Named, sc *oas.Schema) bool {
	if sc == nil {
		return false
	}
	if m, ok := g.methods[n]; ok {
		return m.schema == sc
	}
	m := &method{named: n, schema: sc}
	g.methods[n] = m
	g.queue = append(g.queue, m)
	return true
}

func (g *generator) localNamed(t types.Type) *types.Named {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() != g.gp.pkg || n.TypeParams().Len() > 0 || n.Origin() != n {
		return nil
	}
	return n
}

// regenerateFix is the fix when the Go package does not match the spec.
const regenerateFix = "regenerate the oapi-codegen package from this spec with strict-server: true, " +
	"or pass the -spec it was generated from"

// errorf records a problem in the spec. Its location is the JSON Pointer at
// the start of the message; the spec line is not known here.
func (g *generator) errorf(fix, format string, args ...any) {
	g.errs = append(g.errs, gen.Errorf(g.spec.Path, 0, fix, format, args...))
}

// goErrorf records a problem at a position in the Go package.
func (g *generator) goErrorf(pos token.Pos, fix, format string, args ...any) {
	at := g.gp.fset.Position(pos)
	g.errs = append(g.errs, gen.Errorf(relWD(at.Filename), at.Line, fix, format, args...))
}

// relWD shortens an absolute path to one relative to the working directory,
// as the paths a caller passes are.
func relWD(name string) string {
	wd, err := os.Getwd()
	if err != nil {
		return name
	}
	if rel, relErr := filepath.Rel(wd, name); relErr == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return name
}

func (g *generator) diag(loc, msg string) {
	g.diags[Diagnostic{Loc: loc, Message: msg}] = true
}

func (g *generator) diagnostics() []Diagnostic {
	out := make([]Diagnostic, 0, len(g.diags))
	for d := range g.diags {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Loc != out[j].Loc {
			return out[i].Loc < out[j].Loc
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func (g *generator) fresh(prefix string) string {
	g.n++
	return prefix + strconv.Itoa(g.n)
}
